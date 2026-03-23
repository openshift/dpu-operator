
FROM localhost/dpu-operator:dev-base-arm64

ARG TARGETARCH

COPY  bin/manager.${TARGETARCH} /manager
