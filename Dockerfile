# Build the manager binary
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.24-bookworm@sha256:1a6d4452c65dea36aac2e2d606b01b4a029ec90cc1ae53890540ce6173ea77ac AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
COPY . .

# Build directly to avoid GOARCH leaking into go-run helper tooling during cross builds.
RUN mkdir -p /workspace/bin && \
    GOMAXPROCS=2 CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -o /workspace/bin/manager.${TARGETARCH} ./cmd/main.go

# Use a minimal runtime image for the manager binary.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:a9329520abc449e3b14d5bc3a6ffae065bdde0f02667fa10880c49b35c109fd1
WORKDIR /
ARG TARGETARCH
COPY --from=builder /workspace/bin/manager.${TARGETARCH} /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
