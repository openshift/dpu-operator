# Copyright (c) 2023 Intel Corporation

.PHONY: all fmt check-fmt vet build ipuplugin test update-mod ipuplugin-amd64 ipuplugin-arm64
APP_NAME = ipuplugin
P4_NAME  ?= fxp-net_linux-networking
P4_DIR   ?= fxp-net_linux-networking
VERSION ?= 0.0.0
IMAGE_NAME = intel-$(APP_NAME)
ifneq (, $(IMAGE_REGISTRY))
IMAGE_TAG_BASE = $(IMAGE_REGISTRY)/$(IMAGE_NAME)
else
IMAGE_TAG_BASE = $(IMAGE_NAME)
endif
IMAGE_TAG_LATEST?=$(IMAGE_TAG_BASE):latest
IMAGE_TAG_VERSION=$(IMAGE_TAG_BASE):$(VERSION)
IMGTOOL ?= docker

DOCKERFILE?=$(CURDIR)/images/Dockerfile
DOCKERARGS=
ifdef HTTP_PROXY
	DOCKERARGS += --build-arg http_proxy=$(HTTP_PROXY)
endif
ifdef HTTPS_PROXY
	DOCKERARGS += --build-arg https_proxy=$(HTTPS_PROXY)
endif
DOCKERARGS += --build-arg P4_NAME=$(P4_NAME)

all: check-fmt lint update-mod build

fmt:
	go fmt ./...

check-fmt: ## Check go formatting issues against code.
	./hack/cicd/check-go-fmt.sh

vet:
	go vet ./...

LINTER = $$(go env GOPATH)/bin/golangci-lint
lint: golangci-lint
	$(LINTER) run --timeout 2m0s

build: ipuplugin-amd64 ipuplugin-arm64

update-mod:
	go mod tidy

ipuplugin-amd64:
	env GOOS=linux GOARCH=amd64 go build -o ./bin/linux-amd64/ipuplugin ./ipuplugin/main.go

ipuplugin-arm64:
	env GOOS=linux GOARCH=arm64 go build -o ./bin/linux-arm64/ipuplugin ./ipuplugin/main.go

test:
	@go test -cover ./...

image:
	cp -r ../e2e/artefacts/$(P4_DIR) $(CURDIR)
	mkdir -p $(CURDIR)/bin && cp -r ../e2e/artefacts/bin/* $(CURDIR)/bin/
	$(IMGTOOL) build -t $(IMAGE_TAG_VERSION) -f $(DOCKERFILE)  $(CURDIR) $(DOCKERARGS) --no-cache
	$(IMGTOOL) image tag $(IMAGE_TAG_VERSION) $(IMAGE_TAG_LATEST)

PLATFORMS ?= linux/arm64,linux/amd64
# https://stackoverflow.com/questions/73210141/running-buildkit-using-docker-buildx-behind-a-proxy
# 	docker buildx create --use --driver-opt env.http_proxy=$(HTTP_PROXY) --driver-opt env.https_proxy=$(HTTPS_PROXY) --driver-opt env.no_proxy=$(NO_PROXY)
# buildkit.toml file is a configuration file which allows to set registry configuration
imagex: ## Build and push docker image for the manager for cross-platform support
	cp -r ../e2e/artefacts/$(P4_DIR) $(CURDIR)
	mkdir -p $(CURDIR)/bin && cp -r ../e2e/artefacts/bin/* $(CURDIR)/bin/
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' $(DOCKERFILE) > $(DOCKERFILE).cross
	- $(IMGTOOL) buildx create --name image-builder --use --config=buildkit.toml --buildkitd-flags '--allow-insecure-entitlement security.insecure network.host' --driver-opt env.http_proxy=$(HTTP_PROXY) --driver-opt env.https_proxy=$(HTTPS_PROXY) --driver-opt '"env.no_proxy='$(NO_PROXY)'"'
	- $(IMGTOOL) buildx use image-builder
	- $(IMGTOOL) buildx build --allow security.insecure --push --platform=$(PLATFORMS) --tag ${IMAGE_TAG_VERSION} --tag ${IMAGE_TAG_LATEST} -f $(DOCKERFILE).cross  $(CURDIR) $(DOCKERARGS)
	- $(IMGTOOL) buildx rm image-builder
	rm $(DOCKERFILE).cross

clean:
	@echo "Remove bin directory"
	rm -rf ./bin

# golangci-lint is not recommended to install via 'go get..' or 'go install..'. Ref:  https://golangci-lint.run/usage/install/
golangci-lint: ## Download golangci-lint locally if necessary.
ifeq (,$(wildcard $(LINTER)))
ifeq (,$(shell which golangci-lint 2>/dev/null))
	@{ \
	set -e ;\
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin; \
	}
else
LINTER = $(shell which golangci-lint)
endif
endif
