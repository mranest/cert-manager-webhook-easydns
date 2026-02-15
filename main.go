package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
)

var GroupName = os.Getenv("GROUP_NAME")

func main() {
	if GroupName == "" {
		panic("GROUP_NAME must be specified")
	}

	// This will register our custom DNS provider with the webhook serving
	// library, making it available as an API under the provided GroupName.
	// You can register multiple DNS provider implementations with a single
	// webhook, where the Name() method will be used to disambiguate between
	// the different implementations.
	cmd.RunWebhookServer(GroupName,
		&easyDNSSolver{},
	)
}

// easyDNSSolver implements the provider-specific logic needed to
// 'present' an ACME challenge TXT record for your own DNS provider.
// To do so, it must implement the `github.com/cert-manager/cert-manager/pkg/acme/webhook.Solver`
// interface.
type easyDNSSolver struct {
	secretsClient v1.SecretsGetter
	httpClient    *http.Client
	debugHTTP     bool
}

// easyDNSConfig is a structure that is used to decode into when
// solving a DNS01 challenge.
// This information is provided by cert-manager, and may be a reference to
// additional configuration that's needed to solve the challenge for this
// particular certificate or issuer.
// This typically includes references to Secret resources containing DNS
// provider credentials, in cases where a 'multi-tenant' DNS solver is being
// created.
// If you do *not* require per-issuer or per-certificate configuration to be
// provided to your webhook, you can skip decoding altogether in favour of
// using CLI flags or similar to provide configuration.
// You should not include sensitive information here. If credentials need to
// be used by your provider here, you should reference a Kubernetes Secret
// resource and fetch these credentials using a Kubernetes clientset.
type easyDNSConfig struct {
	TokenSecretRef cmmeta.SecretKeySelector `json:"tokenSecretRef"`
	KeySecretRef   cmmeta.SecretKeySelector `json:"keySecretRef"`
	Endpoint       string                   `json:"endpoint,omitempty"`
	TTL            int                      `json:"ttl,omitempty"`
}

// Name is used as the name for this DNS solver when referencing it on the ACME
// Issuer resource.
// This should be unique **within the group name**, i.e. you can have two
// solvers configured with the same Name() **so long as they do not co-exist
// within a single webhook deployment**.
// For example, `cloudflare` may be used as the name of a solver.
func (c *easyDNSSolver) Name() string {
	return "easydns"
}

// Present is responsible for actually presenting the DNS record with the
// DNS provider.
// This method should tolerate being called multiple times with the same value.
// cert-manager itself will later perform a self check to ensure that the
// solver has correctly configured the DNS provider.
func (c *easyDNSSolver) Present(ch *v1alpha1.ChallengeRequest) error {
	client, zone, host, records, cfg, err := c.prepareChallenge(ch)
	if err != nil {
		return err
	}
	for _, record := range records {
		if !strings.EqualFold(record.Type, "TXT") {
			continue
		}
		if !sameHost(record.Host, host) {
			continue
		}
		if record.RData == ch.Key {
			return nil
		}
	}
	ttl := "0"
	if cfg.TTL > 0 {
		ttl = strconv.Itoa(cfg.TTL)
	}
	return client.addTXT(zone, host, ttl, ch.Key)
}

// CleanUp should delete the relevant TXT record from the DNS provider console.
// If multiple TXT records exist with the same record name (e.g.
// _acme-challenge.example.com) then **only** the record with the same `key`
// value provided on the ChallengeRequest should be cleaned up.
// This is in order to facilitate multiple DNS validations for the same domain
// concurrently.
func (c *easyDNSSolver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	client, zone, host, records, _, err := c.prepareChallenge(ch)
	if err != nil {
		return err
	}
	for _, record := range records {
		if !strings.EqualFold(record.Type, "TXT") {
			continue
		}
		if !sameHost(record.Host, host) {
			continue
		}
		if record.RData != ch.Key {
			continue
		}
		if err := client.deleteRecord(zone, record.ID); err != nil {
			return err
		}
	}
	return nil
}

// Initialize will be called when the webhook first starts.
// This method can be used to instantiate the webhook, i.e. initialising
// connections or warming up caches.
// Typically, the kubeClientConfig parameter is used to build a Kubernetes
// client that can be used to fetch resources from the Kubernetes API, e.g.
// Secret resources containing credentials used to authenticate with DNS
// provider accounts.
// The stopCh can be used to handle early termination of the webhook, in cases
// where a SIGTERM or similar signal is sent to the webhook process.
func (c *easyDNSSolver) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return err
	}
	c.secretsClient = cl.CoreV1()
	c.httpClient = &http.Client{Timeout: 30 * time.Second}
	c.debugHTTP = strings.EqualFold(os.Getenv("EASYDNS_DEBUG_HTTP"), "true") || os.Getenv("EASYDNS_DEBUG_HTTP") == "1"
	_ = stopCh
	return nil
}

// loadConfig is a small helper function that decodes JSON configuration into
// the typed config struct.
func loadConfig(cfgJSON *extapi.JSON) (easyDNSConfig, error) {
	cfg := easyDNSConfig{}
	// handle the 'base case' where no configuration has been provided
	if cfgJSON == nil {
		return cfg, nil
	}
	if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
		return cfg, fmt.Errorf("error decoding solver config: %v", err)
	}

	return cfg, nil
}

func (c *easyDNSSolver) prepareChallenge(ch *v1alpha1.ChallengeRequest) (*easyDNSClient, string, string, []easyDNSRecord, easyDNSConfig, error) {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return nil, "", "", nil, easyDNSConfig{}, err
	}
	token, key, err := c.loadCredentials(ch.ResourceNamespace, cfg)
	if err != nil {
		return nil, "", "", nil, easyDNSConfig{}, err
	}
	client := newEasyDNSClient(c.httpClient, cfg.Endpoint, token, key)
	client.debug = c.debugHTTP
	zone, records, err := client.resolveZoneAndRecords(ch.ResolvedZone, ch.ResolvedFQDN)
	if err != nil {
		return nil, "", "", nil, easyDNSConfig{}, err
	}
	host, err := relativeHost(ch.ResolvedFQDN, zone)
	if err != nil {
		return nil, "", "", nil, easyDNSConfig{}, err
	}
	return client, zone, host, records, cfg, nil
}

func (c *easyDNSClient) resolveZoneAndRecords(resolvedZone, fqdn string) (string, []easyDNSRecord, error) {
	zone := strings.TrimSuffix(strings.TrimSpace(resolvedZone), ".")
	if zone != "" {
		records, err := c.listRecords(zone)
		if err != nil {
			return "", nil, fmt.Errorf("resolved zone %q record lookup failed: %w", zone, err)
		}
		return zone, records, nil
	}
	return c.findZone(fqdn)
}

func (c *easyDNSSolver) loadCredentials(namespace string, cfg easyDNSConfig) (string, string, error) {
	if c.secretsClient == nil {
		return "", "", fmt.Errorf("solver not initialized")
	}
	token, err := c.loadSecretKey(namespace, cfg.TokenSecretRef)
	if err != nil {
		return "", "", err
	}
	key, err := c.loadSecretKey(namespace, cfg.KeySecretRef)
	if err != nil {
		return "", "", err
	}
	return token, key, nil
}

func (c *easyDNSSolver) loadSecretKey(namespace string, ref cmmeta.SecretKeySelector) (string, error) {
	if ref.Name == "" || ref.Key == "" {
		return "", fmt.Errorf("secret reference must include both name and key")
	}
	secret, err := c.secretsClient.Secrets(namespace).Get(context.Background(), ref.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("error getting secret %s/%s: %w", namespace, ref.Name, err)
	}
	value, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("secret %s/%s does not contain key %s", namespace, ref.Name, ref.Key)
	}
	return strings.TrimSpace(string(value)), nil
}

type easyDNSClient struct {
	httpClient *http.Client
	baseURL    string
	token      string
	key        string
	debug      bool
}

type easyDNSRecord struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
	Host   string `json:"host"`
	Type   string `json:"type"`
	RData  string `json:"rdata"`
}

type easyDNSResponse struct {
	Status json.RawMessage `json:"status"`
	Msg    string          `json:"msg"`
	Data   json.RawMessage `json:"data"`
}

func newEasyDNSClient(httpClient *http.Client, endpoint, token, key string) *easyDNSClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL := strings.TrimSpace(endpoint)
	if baseURL == "" {
		baseURL = "https://rest.easydns.net"
	}
	return &easyDNSClient{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		key:        key,
	}
}

func (c *easyDNSClient) findZone(fqdn string) (string, []easyDNSRecord, error) {
	name := strings.TrimSuffix(strings.TrimSpace(fqdn), ".")
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return "", nil, fmt.Errorf("resolved FQDN %q is not a valid domain name", fqdn)
	}
	var lastErr error
	for i := 0; i < len(parts)-1; i++ {
		zone := strings.Join(parts[i:], ".")
		records, err := c.listRecords(zone)
		if err != nil {
			lastErr = err
			continue
		}
		return zone, records, nil
	}
	if lastErr != nil {
		return "", nil, fmt.Errorf("could not discover zone for %q: %w", fqdn, lastErr)
	}
	return "", nil, fmt.Errorf("could not discover zone for %q", fqdn)
}

func (c *easyDNSClient) listRecords(zone string) ([]easyDNSRecord, error) {
	resp, err := c.doJSON(http.MethodGet, fmt.Sprintf("/zones/records/all/%s?format=json", zone), nil)
	if err != nil {
		return nil, err
	}
	var records []easyDNSRecord
	if len(resp.Data) == 0 || string(resp.Data) == "null" {
		return records, nil
	}
	if err := json.Unmarshal(resp.Data, &records); err != nil {
		return nil, fmt.Errorf("error decoding record list for zone %s: %w", zone, err)
	}
	return records, nil
}

func (c *easyDNSClient) addTXT(zone, host, ttl, value string) error {
	payload := map[string]string{
		"domain": zone,
		"host":   host,
		"ttl":    ttl,
		"prio":   "0",
		"type":   "TXT",
		"rdata":  value,
	}
	_, err := c.doJSON(http.MethodPut, fmt.Sprintf("/zones/records/add/%s/txt?format=json", zone), payload)
	if err != nil {
		return fmt.Errorf("error creating TXT record in zone %s: %w", zone, err)
	}
	return nil
}

func (c *easyDNSClient) deleteRecord(zone, recordID string) error {
	if recordID == "" {
		return fmt.Errorf("record id is empty")
	}
	_, err := c.doJSON(http.MethodDelete, fmt.Sprintf("/zones/records/%s/%s?format=json", zone, recordID), nil)
	if err != nil {
		return fmt.Errorf("error deleting record %s in zone %s: %w", recordID, zone, err)
	}
	return nil
}

func (c *easyDNSClient) doJSON(method, path string, body any) (*easyDNSResponse, error) {
	var bodyReader io.Reader
	var requestBody []byte
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("error encoding request body: %w", err)
		}
		requestBody = raw
		bodyReader = bytes.NewReader(raw)
	}
	if c.debug {
		if len(requestBody) == 0 {
			fmt.Printf("[easydns] %s %s\n", method, c.baseURL+path)
		} else {
			fmt.Printf("[easydns] %s %s body=%s\n", method, c.baseURL+path, string(requestBody))
		}
	}
	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.SetBasicAuth(c.token, c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error calling easydns api: %w", err)
	}
	defer func() {
		if closeErr := httpResp.Body.Close(); closeErr != nil && c.debug {
			fmt.Printf("[easydns] response body close error: %v\n", closeErr)
		}
	}()
	rawResp, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading api response: %w", err)
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		if c.debug {
			fmt.Printf("[easydns] response status=%d body=%s\n", httpResp.StatusCode, strings.TrimSpace(string(rawResp)))
		}
		return nil, fmt.Errorf("api returned HTTP %d: %s", httpResp.StatusCode, strings.TrimSpace(string(rawResp)))
	}
	if c.debug {
		fmt.Printf("[easydns] response status=%d body=%s\n", httpResp.StatusCode, strings.TrimSpace(string(rawResp)))
	}
	resp := &easyDNSResponse{}
	if len(bytes.TrimSpace(rawResp)) == 0 {
		return resp, nil
	}
	if err := json.Unmarshal(rawResp, resp); err != nil {
		return nil, fmt.Errorf("error decoding api response JSON: %w", err)
	}
	if !isSuccessStatus(resp.Status) {
		return nil, fmt.Errorf("api error: %s", strings.TrimSpace(string(rawResp)))
	}
	return resp, nil
}

func isSuccessStatus(status json.RawMessage) bool {
	if len(status) == 0 {
		return true
	}
	var boolStatus bool
	if err := json.Unmarshal(status, &boolStatus); err == nil {
		return boolStatus
	}
	var intStatus int
	if err := json.Unmarshal(status, &intStatus); err == nil {
		return intStatus == 200 || intStatus == 201 || intStatus == 204
	}
	var stringStatus string
	if err := json.Unmarshal(status, &stringStatus); err == nil {
		s := strings.ToLower(strings.TrimSpace(stringStatus))
		return s == "ok" || s == "success" || s == "200" || s == "201" || s == "204"
	}
	return false
}

func relativeHost(fqdn, zone string) (string, error) {
	name := strings.TrimSuffix(strings.TrimSpace(fqdn), ".")
	zoneName := strings.TrimSuffix(strings.TrimSpace(zone), ".")
	if strings.EqualFold(name, zoneName) {
		return "", nil
	}
	suffix := "." + zoneName
	if !strings.HasSuffix(strings.ToLower(name), strings.ToLower(suffix)) {
		return "", fmt.Errorf("fqdn %q is not part of zone %q", fqdn, zone)
	}
	return name[:len(name)-len(suffix)], nil
}

func sameHost(left, right string) bool {
	normalize := func(value string) string {
		value = strings.TrimSpace(value)
		if value == "@" {
			return ""
		}
		return strings.TrimSuffix(value, ".")
	}
	return strings.EqualFold(normalize(left), normalize(right))
}
