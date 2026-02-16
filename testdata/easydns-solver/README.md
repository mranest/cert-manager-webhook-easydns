# Solver testdata directory

`config.json` is loaded by `main_test.go` and passed to the solver for
conformance tests.

Credentials are configured via secret references:

- Use `tokenSecretRef` and `keySecretRef` in `config.json`.
- Create `easydns-credentials.yaml` from `easydns-credentials.yaml.example`
  and set real values.
- `main_test.go` applies YAML manifests in this directory to each test namespace.
- Keep `easydns-credentials.yaml.example` as a template only; it is not applied
  because it does not have a `.yaml` extension.
