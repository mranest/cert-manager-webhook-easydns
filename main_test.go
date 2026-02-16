package main

import (
	"encoding/json"
	"os"
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
	manifestPath := "testdata/easydns-solver"
	config := loadSolverConfig(t, manifestPath+"/config.json")

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

type solverConfig struct {
	JSON extapi.JSON
}

func loadSolverConfig(t *testing.T, path string) solverConfig {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading test config %q: %v", path, err)
	}

	cfg := easyDNSConfig{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decoding test config %q: %v", path, err)
	}

	normalized, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("encoding solver config: %v", err)
	}
	return solverConfig{
		JSON: extapi.JSON{Raw: normalized},
	}
}
