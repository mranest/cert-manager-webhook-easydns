GO ?= $(shell which go)
OS ?= $(shell $(GO) env GOOS)
ARCH ?= $(shell $(GO) env GOARCH)
SETUP_ENVTEST ?= $(shell $(GO) env GOPATH)/bin/setup-envtest
SETUP_ENVTEST_GOBIN ?= $(dir $(SETUP_ENVTEST))

IMAGE_NAME := "webhook"
IMAGE_TAG := "latest"

OUT := $(shell pwd)/_out

ENVTEST_K8S_VERSION ?= 1.28.0

HELM_FILES := $(shell find deploy/example-webhook)

test: setup-envtest
	@mkdir -p _test/bin
	@ASSETS_DIR="$$($(SETUP_ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(shell pwd)/_test/bin -p path)"; \
	TEST_ASSET_ETCD="$$ASSETS_DIR/etcd" \
	TEST_ASSET_KUBE_APISERVER="$$ASSETS_DIR/kube-apiserver" \
	TEST_ASSET_KUBECTL="$$ASSETS_DIR/kubectl" \
	$(GO) test -v .

setup-envtest:
	@command -v $(SETUP_ENVTEST) >/dev/null 2>&1 || { \
		echo "Installing setup-envtest to $(SETUP_ENVTEST)"; \
		GOBIN=$(SETUP_ENVTEST_GOBIN) $(GO) install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest; \
	}

.PHONY: clean
clean:
	rm -rf _test $(OUT)

.PHONY: build
build:
	docker build -t "$(IMAGE_NAME):$(IMAGE_TAG)" .

.PHONY: rendered-manifest.yaml
rendered-manifest.yaml: $(OUT)/rendered-manifest.yaml

$(OUT)/rendered-manifest.yaml: $(HELM_FILES) | $(OUT)
	helm template \
	    --name example-webhook \
            --set image.repository=$(IMAGE_NAME) \
            --set image.tag=$(IMAGE_TAG) \
            deploy/example-webhook > $@

_test $(OUT):
	mkdir -p $@
