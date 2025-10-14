# Copyright (c) 2024 Intel Corporation

APP_NAME = networkfunction
IMAGE_NAME = intel-$(APP_NAME)

IMGTOOL ?= docker

IMAGE_REGISTRY?=localhost:5000
IMAGE_TAG_BASE = $(IMAGE_REGISTRY)/$(IMAGE_NAME)
IMAGE_TAG_LATEST?=$(IMAGE_TAG_BASE):latest
DOCKERFILE?=$(CURDIR)/Dockerfile

DOCKERARGS=
ifdef HTTP_PROXY
	DOCKERARGS += --build-arg http_proxy=$(HTTP_PROXY)
endif
ifdef HTTPS_PROXY
	DOCKERARGS += --build-arg https_proxy=$(HTTPS_PROXY)
endif

all: image push 

build: 
	mkdir -p bin/
	gcc -o bin/nf nf.c

clean:
	@echo "Remove bin directory"
	rm -rf ./bin

image:
	$(IMGTOOL) build -t $(IMAGE_TAG_LATEST) -f $(DOCKERFILE) $(CURDIR) $(DOCKERARGS) --no-cache

push:
	$(IMGTOOL) push $(IMAGE_TAG_LATEST)
