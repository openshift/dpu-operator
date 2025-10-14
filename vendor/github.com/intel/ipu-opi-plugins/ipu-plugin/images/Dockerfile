# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2024 Intel Corporation
FROM golang:1.22.5-alpine@sha256:0d3653dd6f35159ec6e3d10263a42372f6f194c3dea0b35235d72aabde86486e AS builder
RUN apk add --no-cache git
ARG TARGETOS
ARG TARGETARCH

RUN apk add --no-cache --virtual build-dependencies build-base
COPY . /usr/src/ipu-opi-plugin
WORKDIR /usr/src/ipu-opi-plugin
RUN go mod download
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o bin/ipuplugin ipuplugin/main.go

FROM alpine:3@sha256:c5b1261d6d3e43071626931fc004f70149baeba2c8ec672bd4f27761f8e1ad6b
ARG P4_NAME
ENV P4_NAME=$P4_NAME

COPY --from=builder /usr/src/ipu-opi-plugin/bin/ipuplugin /usr/bin/
COPY ${P4_NAME}/${P4_NAME}.pkg /
RUN mkdir -p /opt/p4/p4-cp-nws/bin/p4
COPY bin/p4rt-ctl /opt/p4/p4-cp-nws/bin/
#TODO: Update to newer p4runtime, for latest release.
COPY bin/p4runtime-2023.11.0/p4 /opt/p4/p4-cp-nws/bin/p4
COPY bin/p4runtime-2023.11.0/copy_p4rt_python_deps.sh /opt/p4/p4-cp-nws/bin/p4

WORKDIR /
LABEL io.k8s.display-name="IPU OPI Plugin"
ENV PYTHONUNBUFFERED=1
#Install deps for p4runtime(previously deps got installed per p4runtime wheel package's->p4runtime-2023.11.0.dist-info/METADATA)
RUN apk add -U --no-cache python3'>='3.8  py3-pip'=='23.3.1-r0 \
    py3-grpcio'=='1.59.3-r0 \
    py3-netaddr'=='0.9.0-r0

#Install deps for p4runtime(previously deps got installed per p4runtime wheel package's->p4runtime-2023.11.0.dist-info/METADATA)
RUN python3 -m pip install --no-cache-dir --break-system-packages -v "protobuf==4.25.0" "googleapis-common-protos==1.66.0"

#Note: Redhat's build system does not allow p4runtime wheel package. Manually installing python protobuf
#files(for p4runtime) under pip's installation path.
RUN chmod a+x /opt/p4/p4-cp-nws/bin/p4/copy_p4rt_python_deps.sh && /opt/p4/p4-cp-nws/bin/p4/copy_p4rt_python_deps.sh

RUN rm -rf /var/cache/apk/*
RUN apk add --update --no-cache openssh networkmanager-cli openvswitch iproute2
