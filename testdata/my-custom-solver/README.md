# Solver testdata directory

`config.example.json` is a reference payload showing all supported webhook
config keys (`tokenSecretRef`, `keySecretRef`, `endpoint`, `ttl`).

The current conformance test setup in `main_test.go` is env-driven and builds
the test config dynamically, so this file is documentation/reference and is not
loaded automatically during `make test`.
