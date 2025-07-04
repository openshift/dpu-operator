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


# Image URL to use all building/pushing image targets
IMG ?= controller:latest



# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: default
default: build

# TODO: remove this when we don't call this target directly anymore
.PHONY: deploy_clusters
deploy_clusters:
	go run tools/task/task.go deploy-clusters

.PHONY: ginkgo
ginkgo:
	go run tools/task/task.go ginkgo

.PHONY: traffic-flow-tests
traffic-flow-tests:
	hack/traffic_flow_tests.sh

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
manifests:
	go run tools/task/task.go manifests

.PHONY: prow-ci-manifests-check
prow-ci-manifests-check: manifests
	go run tools/task/task.go prow-ci-manifests-check

# TODO: Remove when CI uses go-task instead
.PHONY: vendor
vendor:
	go run tools/task/task.go vendor

# TODO: Remove when CI uses go-task instead
.PHONY: generate
generate:
	go run tools/task/task.go generate

# TODO: Remove when CI uses go-task instead
.PHONY: generate-check
generate-check: controller-gen
	./scripts/check-gittree-for-diff.sh make generate

# TODO: Remove when CI uses go-task instead
.PHONY: vendor-check
vendor-check:
	./scripts/check-gittree-for-diff.sh make vendor

# TODO: Remove when CI uses go-task instead
.PHONY: fmt
fmt: ## Run go fmt against code.
	go run tools/task/task.go fmt

# TODO: Remove when CI uses go-task instead
.PHONY: fmt-check
fmt-check:
	go run tools/task/task.go fmt-check

# TODO: Remove when CI uses go-task instead
.PHONY: vet
vet: ## Run go vet against code.
	go run tools/task/task.go vet

# TODO: Remove when CI uses go-task instead
.PHONY: test
test:
	go run tools/task/task.go test

# TODO: Remove when CI uses go-task instead
.PHONY: fast-test
fast-test:
	go run tools/task/task.go fast-test

##@ Build


.PHONY: build
build: manifests generate fmt vet build-manager build-daemon build-intel-vsp build-marvell-vsp build-intel-netsec-vsp build-network-resources-injector
	@echo "Built all components"

.PHONY: build-manager
build-manager:
	go run tools/task/task.go build-bin-manager

.PHONY: build-daemon
build-daemon:
	go run tools/task/task.go build-bin-daemon

.PHONY: build-intel-vsp
build-intel-vsp:
	go run tools/task/task.go build-bin-intel-vsp

.PHONY: build-marvell-vsp
build-marvell-vsp:
	go run tools/task/task.go build-bin-marvell-vsp

.PHONY: build-intel-netsec-vsp
build-intel-netsec-vsp:
	go run tools/task/task.go build-bin-intel-netsec-vsp

.PHONY: build-network-resources-injector
build-network-resources-injector:
	go run tools/task/task.go build-bin-network-resources-injector


.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -
	@echo "Waiting for namespace 'openshift-dpu-operator' to be removed..."
	@while $(KUBECTL) get ns openshift-dpu-operator >/dev/null 2>&1; do \
		echo "Namespace still exists... waiting"; \
		sleep 5; \
	done
	@echo "Namespace 'openshift-dpu-operator' has been removed."


##@ Build Dependencies


## Tool Binaries
KUBECTL ?= oc
TASK_BINDIR := $(shell go run tools/task/task.go bindir)
KUSTOMIZE ?= $(TASK_BINDIR)/kustomize
OPERATOR_SDK ?= $(TASK_BINDIR)/operator-sdk
OPM = $(TASK_BINDIR)/opm


.PHONY: kustomize
kustomize:
	go run tools/task/task.go kustomize

.PHONY: controller-gen
controller-gen:
	go run tools/task/task.go controller-gen

.PHONY: operator-sdk
operator-sdk:
	go run tools/task/task.go operator-sdk

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
opm:
	go run tools/task/task.go opm

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

