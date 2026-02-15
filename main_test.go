package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	acmetest "github.com/cert-manager/cert-manager/test/acme"
)

var (
	zone = os.Getenv("TEST_ZONE_NAME")
)

func TestRunsSuite(t *testing.T) {
	if zone == "" {
		t.Skip("TEST_ZONE_NAME must be set, e.g. example.com.")
	}
	token := os.Getenv("EASYDNS_TOKEN")
	key := os.Getenv("EASYDNS_KEY")
	if token == "" || key == "" {
		t.Skip("EASYDNS_TOKEN and EASYDNS_KEY must be set")
	}
	config := buildSolverConfig(t)
	manifestPath := buildManifestPath(t, token, key, config.SecretName)

	opts := []acmetest.Option{
		acmetest.SetResolvedZone(zone),
		acmetest.SetAllowAmbientCredentials(false),
		acmetest.SetManifestPath(manifestPath),
		acmetest.SetConfig(config.JSON),
	}
	if value := os.Getenv("TEST_RESOLVED_FQDN"); value != "" {
		opts = append(opts, acmetest.SetResolvedFQDN(value))
	}
	if value := os.Getenv("TEST_DNS_NAME"); value != "" {
		opts = append(opts, acmetest.SetDNSName(value))
	}
	if value := os.Getenv("TEST_DNS_SERVER"); value != "" {
		opts = append(opts, acmetest.SetDNSServer(value))
	}
	if value := os.Getenv("TEST_USE_AUTHORITATIVE"); value != "" {
		useAuthoritative, err := strconv.ParseBool(value)
		if err != nil {
			t.Fatalf("invalid TEST_USE_AUTHORITATIVE value %q: %v", value, err)
		}
		opts = append(opts, acmetest.SetUseAuthoritative(useAuthoritative))
	}
	if value := os.Getenv("TEST_POLL_INTERVAL"); value != "" {
		pollInterval, err := time.ParseDuration(value)
		if err != nil {
			t.Fatalf("invalid TEST_POLL_INTERVAL value %q: %v", value, err)
		}
		opts = append(opts, acmetest.SetPollInterval(pollInterval))
	}
	if value := os.Getenv("TEST_PROPAGATION_LIMIT"); value != "" {
		propagationLimit, err := time.ParseDuration(value)
		if err != nil {
			t.Fatalf("invalid TEST_PROPAGATION_LIMIT value %q: %v", value, err)
		}
		opts = append(opts, acmetest.SetPropagationLimit(propagationLimit))
	}

	fixture := acmetest.NewFixture(&easyDNSSolver{}, opts...)

	fixture.RunConformance(t)
}

func buildManifestPath(t *testing.T, token, key, secretName string) string {
	t.Helper()

	dir := t.TempDir()

	secretManifest := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
type: Opaque
stringData:
  token: %q
  key: %q
`, secretName, token, key)
	if err := os.WriteFile(filepath.Join(dir, "easydns-credentials.yaml"), []byte(secretManifest), 0o600); err != nil {
		t.Fatalf("writing credential manifest: %v", err)
	}

	return dir
}

type solverConfig struct {
	JSON       extapi.JSON
	SecretName string
}

func buildSolverConfig(t *testing.T) solverConfig {
	t.Helper()

	secretName := getEnvOrDefault("EASYDNS_SECRET_NAME", "easydns-credentials")
	tokenKey := getEnvOrDefault("EASYDNS_TOKEN_SECRET_KEY", "token")
	keyKey := getEnvOrDefault("EASYDNS_KEY_SECRET_KEY", "key")

	cfg := easyDNSConfig{
		TokenSecretRef: secretKeyRef{Name: secretName, Key: tokenKey},
		KeySecretRef:   secretKeyRef{Name: secretName, Key: keyKey},
		Endpoint:       os.Getenv("EASYDNS_ENDPOINT"),
		TTL:            300,
	}
	if value := os.Getenv("EASYDNS_TTL"); value != "" {
		ttl, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("invalid EASYDNS_TTL value %q: %v", value, err)
		}
		cfg.TTL = ttl
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("encoding solver config: %v", err)
	}
	return solverConfig{
		JSON:       extapi.JSON{Raw: raw},
		SecretName: secretName,
	}
}

func getEnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
