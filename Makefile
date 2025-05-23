# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 4.19.0

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# openshift.io/dpu-operator-bundle:$VERSION and openshift.io/dpu-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= openshift.io/dpu-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Set the Operator SDK version to use. By default, what is installed on the system is used.
# This is useful for CI or a project to utilize a specific version of the operator-sdk toolkit.
OPERATOR_SDK_VERSION ?= v1.37.0

# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.27.1

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= podman

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: default
default: build

SUBMODULES ?= true

.PHONY: prepare-e2e-test
prepare-e2e-test:
ifeq ($(SUBMODULES), true)
	hack/prepare-submodules.sh
endif
	hack/prepare-venv.sh

# TODO: remove this when we don't call this target directly anymore
.PHONY: deploy_clusters
deploy_clusters:
	go run tools/task/task.go deploy-clusters
	go run tools/task/task.go redeploy

.PHONY: ginkgo
ginkgo:
	go run tools/task/task.go ginkgo

.PHONY: traffic-flow-tests
traffic-flow-tests:
	hack/traffic_flow_tests.sh

.PHONY: e2e_test
e2e-test:
	go run tools/task/task.go e2e-test

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	GOFLAGS='' $(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: prow-ci-manifests-check
prow-ci-manifests-check: manifests
	@changed_files=$$(git diff --name-only); \
	for file in $$changed_files; do \
		diff_output=$$(git diff -- $$file); \
		echo "$$diff_output"; \
	done; \
	if [ -n "$$changed_files" ]; then \
		echo "Please run 'make manifests', the following files changed: $$changed_files"; \
		exit 1; \
	fi

.PHONY: vendor
vendor:
	for d in . dpu-api api tools ; do \
		if [ "$$d" = . ] ; then \
			(cd $$d && go mod vendor) || exit $$? ; \
		fi ; \
		(cd $$d && go mod tidy) || exit $$? ; \
	done

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	GOFLAGS='' $(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: generate-check
generate-check: controller-gen
	./scripts/check-gittree-for-diff.sh make generate

.PHONY: vendor-check
vendor-check:
	./scripts/check-gittree-for-diff.sh make vendor

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...
	cd api && go fmt ./...

.PHONY: fmt-check
fmt-check:
	@files=$$(find . -name "*.go" -not -path "./vendor/*" -not -path "./dpu-api/vendor/*"); \
	output=$$(gofmt -d $$files); \
	[ -n "$$output" ] && echo "$$output"; \
	[ -z "$$output" ]

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: podman-check
check-podman:
	@if which podman > /dev/null; then \
		echo "Podman is available"; \
		else \
		echo "Error: Podman is not available"; \
		exit 1; \
	fi

.PHONY: test
test: podman-check manifests generate fmt vet envtest ginkgo
	FAST_TEST=false KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" timeout 30m $(GINKGO) --repeat 4 $(if $(TEST_FOCUS),-focus $(TEST_FOCUS),) -coverprofile cover.out ./internal/... ./pkgs/... ./api/v1/...

.PHONY: fast-test
fast-test: envtest ginkgo
	FAST_TEST=true KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" $(GINKGO) $(if $(TEST_FOCUS),-focus $(TEST_FOCUS),) -coverprofile cover.out ./internal/... ./pkgs/... ./api/v1/...

##@ Build

MANAGER_BIN     = bin/manager
DAEMON_BIN      = bin/daemon
DPU_CNI_BIN     = bin/dpu-cni
IPU_PLUGIN_BIN  = bin/ipuplugin
VSP_BIN         = bin/vsp-mrvl
NRI_BIN		= bin/nri

GOARCH ?= amd64
GOOS ?= linux

.PHONY: build
build: manifests generate fmt vet build-manager build-daemon build-intel-vsp build-marvell-vsp build-network-resources-injector
	@echo "Built all components"

.PHONY: build-manager
build-manager:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} go build -o $(MANAGER_BIN).${GOARCH} cmd/main.go

.PHONY: build-daemon
build-daemon:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} go build -o $(DAEMON_BIN).${GOARCH} cmd/daemon/daemon.go
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} go build -o $(DPU_CNI_BIN).${GOARCH} dpu-cni/dpu-cni.go

.PHONY: build-intel-vsp
build-intel-vsp:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} go build -o $(IPU_PLUGIN_BIN).${GOARCH} cmd/intelvsp/intelvsp.go

.PHONY: build-marvell-vsp
build-marvell-vsp:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} go build -o $(VSP_BIN).${GOARCH} internal/daemon/vendor-specific-plugins/marvell/main.go

.PHONY: build-network-resources-injector
build-network-resources-injector:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} go build -o ${NRI_BIN}.${GOARCH} cmd/nri/networkresourcesinjector.go

# If you wish built the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64 ). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: test ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

GO_CONTAINER_CACHE = /tmp/dpu-operator-cache
REGISTRY ?= $(shell hostname)
# Use the image urls from the yaml that is used with Kustomize for local
# development.
DPU_OPERATOR_IMAGE := $(REGISTRY):5000/dpu-operator:dev
DPU_DAEMON_IMAGE := $(REGISTRY):5000/dpu-daemon:dev
MARVELL_VSP_IMAGE := $(REGISTRY):5000/mrvl-vsp:dev
INTEL_VSP_IMAGE := $(REGISTRY):5000/intel-vsp:dev
INTEL_VSP_P4_IMAGE := $(REGISTRY):5000/intel-vsp-p4:dev
NETWORK_RESOURCES_INJECTOR_IMAGE:= $(REGISTRY):5000/network-resources-injector-image:dev

.PHONY: prep-local-deploy
prep-local-deploy:
	mkdir -p bin
	go run ./tools/config/config.go -registry-url $(REGISTRY) -template-file config/dev/local-images-template.yaml -output-file bin/local-images.yaml
	cp config/dev/kustomization.yaml bin

.PHONY: incremental-prep-local-deploy
incremental-prep-local-deploy:
	go run ./tools/config/config.go -registry-url $(REGISTRY) -template-file config/incremental/local-images-template.yaml -output-file bin/local-images.yaml
	cp config/dev/kustomization.yaml bin

.PHONY: local-deploy
local-deploy: prep-local-deploy manifests kustomize ## Deploy controller with images hosted on local registry
	-$(MAKE) undeploy
	$(KUSTOMIZE) build bin | $(KUBECTL) apply -f -
	$(KUBECTL) -n openshift-dpu-operator wait --for=condition=ready pod --all --timeout=120s

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -
	@echo "Waiting for namespace 'openshift-dpu-operator' to be removed..."
	@while $(KUBECTL) get ns openshift-dpu-operator >/dev/null 2>&1; do \
		echo "Namespace still exists... waiting"; \
		sleep 5; \
	done
	@echo "Namespace 'openshift-dpu-operator' has been removed."

.PHONY: go-cache
go-cache: ## Build all container images necessary to run the whole operator
	mkdir -p $(GO_CONTAINER_CACHE)

## Build all container images necessary to run the whole operator
# PLATFORMS defines the target platforms for  the manager image be build to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - able to use docker buildx . More info: https://docs.docker.com/build/buildx/
# - have enable BuildKit, More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image for your registry (i.e. if you do not inform a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To properly provided solutions that supports more than one platform you should use this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: test ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name project-v3-builder
	$(CONTAINER_TOOL) buildx use project-v3-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm project-v3-builder
	rm Dockerfile.cross

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= oc
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GINKGO ?= $(LOCALBIN)/ginkgo

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.15.0

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) GOFLAGS='' go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) GOFLAGS='' go install sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.16

.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (, $(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH} ;\
	chmod +x $(OPERATOR_SDK) ;\
	}
else
OPERATOR_SDK = $(shell which operator-sdk)
endif
endif

.PHONY: bundle
bundle: manifests kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(OPERATOR_SDK) bundle validate ./bundle
	cp bundle/manifests/* manifests/stable

.PHONY: prow-ci-bundle-check
prow-ci-bundle-check: bundle
	@changed_files=$$(git diff --name-only); \
	non_timestamp_change=0; \
	for file in $$changed_files; do \
		diff_output=$$(git diff -U0 -- $$file); \
		if echo "$$diff_output" | grep '^[+-]' | grep -Ev '^(--- a/|\+\+\+ b/)' | grep -v "createdAt" | grep -q "."; then \
			echo "$$diff_output"; \
			non_timestamp_change=1; \
		fi; \
	done; \
	if [ $$non_timestamp_change -ne 0 ]; then \
		echo "Please run 'make bundle', detected non timestamp changes"; \
		exit 1; \
	fi

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: sequence-diagrams
sequence-diagrams:
	for f in doc/*.puml; do plantuml -tpng $$f; done

.PHONY: opm
OPM = $(LOCALBIN)/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

proto:
	rm -rf dpu-api/gen
	mkdir -p dpu-api/gen
	cd dpu-api && protoc --go_out=gen --go_opt=paths=source_relative \
		--go-grpc_out=gen --go-grpc_opt=paths=source_relative \
		api.proto
