# cert-manager EasyDNS webhook

This repository implements a cert-manager ACME DNS01 webhook solver for [EasyDNS](https://easydns.com).

The webhook solver name is `easydns`.

## How it works

- Reads EasyDNS API credentials from a Kubernetes `Secret`.
- Discovers the authoritative zone by querying EasyDNS records for suffixes of the challenge FQDN.
- Creates TXT records through the EasyDNS REST API.
- Removes only matching TXT records during cleanup.

The implementation uses these EasyDNS API patterns:

- `GET /zones/records/all/{zone}?format=json`
- `PUT /zones/records/add/{zone}/txt?format=json`
- `DELETE /zones/records/{zone}/{id}?format=json`

## Solver configuration

In your `Issuer`/`ClusterIssuer`, set:

- `tokenSecretRef`: secret ref for EasyDNS API token
- `keySecretRef`: secret ref for EasyDNS API key
- `endpoint` (optional): EasyDNS API base URL (default: `https://rest.easydns.net`)
- `ttl` (optional): TXT record TTL in seconds (default EasyDNS behavior when omitted/0)

## Example Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: easydns-credentials
  namespace: cert-manager
type: Opaque
stringData:
  token: "<easydns-token>"
  key: "<easydns-key>"
```

## Example ClusterIssuer

Replace `groupName` with the chart value you deploy.

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-easydns
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: you@example.com
    privateKeySecretRef:
      name: letsencrypt-easydns
    solvers:
    - dns01:
        webhook:
          groupName: cert-manager-webhook-easydns.mranest.ghcr.io
          solverName: easydns
          config:
            tokenSecretRef:
              name: easydns-credentials
              key: token
            keySecretRef:
              name: easydns-credentials
              key: key
            ttl: 300
```

## Deploy

```bash
helm upgrade --install cert-manager-webhook-easydns ./deploy/webhook-easydns \
  --namespace cert-manager \
  --set image.repository=ghcr.io/mranest/cert-manager-webhook-easydns \
  --set image.tag=latest \
  --set groupName=cert-manager-webhook-easydns.mranest.ghcr.io
```

## Release strategy

This repository uses separate release flows for the webhook image and Helm chart.

- Webhook image:
  - Published automatically by GitHub Actions on repository tags matching `v*`.
  - Uses repository/release versioning tags (for example `v0.1.0`).

- Helm chart:
  - Published manually.
  - Uses its own version in `cert-manager-webhook-easydns/deploy/easydns-webhook/Chart.yaml` (`version` field).
  - Can be released independently of webhook image tags.

Recommended practice:

- When webhook code changes, publish a new image tag and update chart `appVersion` and default image tag accordingly.
- When only chart/templates/values change, bump chart `version` and publish chart without requiring a new webhook image.

## Notes

- The Helm chart grants this webhook permission to `get` `Secrets`, because credential refs are resolved at runtime.
- For `ClusterIssuer`, place the credentials secret in cert-manager's cluster resource namespace (commonly `cert-manager`).

## Running conformance tests

`cert-manager-webhook-easydns/main_test.go` is configured to run cert-manager DNS01 conformance tests against the EasyDNS solver.

Tests are env-driven. The fixture config is generated at runtime from environment
variables, and `cert-manager-webhook-easydns/testdata/my-custom-solver/config.example.json`
is kept as reference for supported config keys.

Prerequisites:

- Set `TEST_ZONE_NAME` to an EasyDNS-managed zone (for example `example.com.`).
- Set `EASYDNS_TOKEN` and `EASYDNS_KEY` with credentials that can manage that zone.
- Ensure the test environment can reach `https://rest.easydns.net`.
- Ensure network access is available to download envtest binaries (the `make test` target uses `setup-envtest` automatically and exports `TEST_ASSET_ETCD`, `TEST_ASSET_KUBE_APISERVER`, and `TEST_ASSET_KUBECTL`).

Run:

```bash
TEST_ZONE_NAME=example.com. EASYDNS_TOKEN=... EASYDNS_KEY=... make test
```

Optional test env overrides:

- `TEST_RESOLVED_FQDN` (default: `cert-manager-dns01-tests.<TEST_ZONE_NAME>`)
- `TEST_DNS_NAME` (default fixture value: `example.com`)
- `TEST_DNS_SERVER` (override recursive resolver)
- `TEST_USE_AUTHORITATIVE` (`true`/`false`)
- `TEST_POLL_INTERVAL` (Go duration, e.g. `5s`)
- `TEST_PROPAGATION_LIMIT` (Go duration, e.g. `5m`)
- `EASYDNS_ENDPOINT` (default: `https://rest.easydns.net`)
- `EASYDNS_TTL` (default: `300`)
- `EASYDNS_SECRET_NAME` (default: `easydns-credentials`)
- `EASYDNS_TOKEN_SECRET_KEY` (default: `token`)
- `EASYDNS_KEY_SECRET_KEY` (default: `key`)

The conformance suite performs real TXT record operations for DNS01 challenges. Use a dedicated test zone/account.
