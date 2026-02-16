# Helm Chart Publishing (GHCR OCI)

This guide describes how to package and publish the Helm chart to GitHub Container Registry (GHCR) as an OCI artifact.

## Prerequisites

- `helm` v3.8+ (OCI support)
- A GitHub Personal Access Token (PAT) with package write permissions
- Access to the target GHCR namespace

## 1. Set chart version metadata

Update `/Users/mranest/src/go/cert-manager-webhook-easydns/deploy/easydns-webhook/Chart.yaml`:

- `version`: chart release version (required for each chart publish)
- `appVersion`: optional, usually set to the webhook image tag

## 2. Authenticate Helm to GHCR

Use placeholders and set real values in your shell:

```bash
export GHCR_USER="<ghcr-owner>"
export GHCR_TOKEN="<github-pat-with-package-write>"
echo "$GHCR_TOKEN" | helm registry login ghcr.io -u "$GHCR_USER" --password-stdin
```

## 3. Package the chart

Run from repository root:

```bash
cd /Users/mranest/src/go/cert-manager-webhook-easydns
helm package ./deploy/easydns-webhook
```

This creates an archive like:

- `cert-manager-webhook-easydns-<chart-version>.tgz`

## 4. Push chart to GHCR

```bash
helm push cert-manager-webhook-easydns-<chart-version>.tgz oci://ghcr.io/<ghcr-owner>/charts
```

## 5. Verify published chart

```bash
helm show chart oci://ghcr.io/<ghcr-owner>/charts/cert-manager-webhook-easydns --version <chart-version>
```

## 6. Install/upgrade from GHCR

```bash
helm upgrade --install cert-manager-webhook-easydns \
  oci://ghcr.io/<ghcr-owner>/charts/cert-manager-webhook-easydns \
  --version <chart-version> \
  --namespace cert-manager \
  --set image.repository=ghcr.io/<ghcr-owner>/cert-manager-webhook-easydns \
  --set image.tag=<image-tag>
```

## Notes

- Chart versioning (`Chart.yaml.version`) can be independent from image versioning.
- If you publish charts manually, ensure `version` is incremented for every publish.
