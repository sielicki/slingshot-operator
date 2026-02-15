# Build the binaries
FROM golang:1.26 AS builder
ARG TARGETOS
ARG TARGETARCH
ARG BINARY=manager

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the Go source (relies on .dockerignore to filter)
COPY . .

# Build all binaries
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager ./cmd/main.go
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o device-plugin ./cmd/device-plugin
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o retry-handler ./cmd/retry-handler
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o driver-agent ./cmd/driver-agent
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o metrics-exporter ./cmd/metrics-exporter

# Manager image - runs the controller
FROM gcr.io/distroless/static:nonroot AS manager
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532
ENTRYPOINT ["/manager"]

# Device plugin image - runs on each node
FROM gcr.io/distroless/static:nonroot AS device-plugin
WORKDIR /
COPY --from=builder /workspace/device-plugin .
USER 65532:65532
ENTRYPOINT ["/device-plugin"]

# Retry handler image - runs as DaemonSet or sidecar
FROM gcr.io/distroless/static:nonroot AS retry-handler
WORKDIR /
COPY --from=builder /workspace/retry-handler .
USER 0:0
ENTRYPOINT ["/retry-handler"]

# Driver agent image - runs privileged on each node
FROM gcr.io/distroless/static:nonroot AS driver-agent
WORKDIR /
COPY --from=builder /workspace/driver-agent .
USER 0:0
ENTRYPOINT ["/driver-agent"]

# Metrics exporter image - runs on each node for Prometheus metrics
FROM gcr.io/distroless/static:nonroot AS metrics-exporter
WORKDIR /
COPY --from=builder /workspace/metrics-exporter .
USER 65532:65532
ENTRYPOINT ["/metrics-exporter"]
