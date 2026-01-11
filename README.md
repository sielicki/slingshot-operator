# Slingshot Operator

[![Tests](https://github.com/sielicki/slingshot-operator/actions/workflows/test.yml/badge.svg)](https://github.com/sielicki/slingshot-operator/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/sielicki/slingshot-operator/branch/main/graph/badge.svg)](https://codecov.io/gh/sielicki/slingshot-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/sielicki/slingshot-operator)](https://goreportcard.com/report/github.com/sielicki/slingshot-operator)

A Kubernetes operator for managing HPE Slingshot CXI (Cassini eXtended Interface) network driver lifecycle on HPC clusters.

## Overview

The Slingshot Operator automates the deployment and management of CXI network drivers and associated components on Kubernetes clusters equipped with HPE Slingshot interconnect hardware. It provides:

- **Driver lifecycle management** - Install, upgrade, and manage CXI kernel drivers via DKMS, prebuilt modules, or preinstalled drivers
- **Device plugin** - Expose CXI devices as Kubernetes extended resources (`hpe.com/cxi`)
- **Retry handler** - Userspace retry handling for network reliability (DaemonSet or sidecar injection)
- **Metrics exporter** - Prometheus metrics for CXI device health, link state, and retry statistics
- **Device discovery** - Automatic discovery and status reporting of CXI devices via `CXIDevice` custom resources

## Architecture

```mermaid
flowchart TB
    subgraph Cluster["Kubernetes Cluster"]
        subgraph ControlPlane["Control Plane"]
            Operator["slingshot-operator<br/>(Deployment)"]
            CXIDriver[("CXIDriver CR<br/>(singleton)")]
            CXIDevice[("CXIDevice CRs<br/>(per device)")]
        end

        subgraph Node1["Node 1"]
            subgraph DS1["DaemonSet Pods"]
                DA1["driver-agent"]
                DP1["device-plugin"]
                RH1["retry-handler"]
                ME1["metrics-exporter"]
            end
            CXI1[["cxi0, cxi1<br/>/dev/cxi*"]]
        end

        subgraph Node2["Node 2"]
            subgraph DS2["DaemonSet Pods"]
                DA2["driver-agent"]
                DP2["device-plugin"]
                RH2["retry-handler"]
                ME2["metrics-exporter"]
            end
            CXI2[["cxi0<br/>/dev/cxi*"]]
        end

        Prometheus["Prometheus"]
    end

    Operator -->|watches| CXIDriver
    Operator -->|creates/updates| CXIDevice
    Operator -->|manages| DS1
    Operator -->|manages| DS2

    DA1 -->|loads driver| CXI1
    DP1 -->|registers| Kubelet1["kubelet"]
    RH1 -->|manages retries| CXI1
    ME1 -->|reads stats| CXI1

    DA2 -->|loads driver| CXI2
    DP2 -->|registers| Kubelet2["kubelet"]
    RH2 -->|manages retries| CXI2
    ME2 -->|reads stats| CXI2

    Prometheus -->|scrapes /metrics| ME1
    Prometheus -->|scrapes /metrics| ME2
```

### Component Responsibilities

```mermaid
flowchart LR
    subgraph Operator["slingshot-operator"]
        direction TB
        R["Reconciler"]
        R --> |"creates"| DaemonSets
        R --> |"updates"| Status
    end

    subgraph DaemonSets["Per-Node DaemonSets"]
        direction TB
        DA["driver-agent<br/>━━━━━━━━━━━━<br/>• DKMS build<br/>• Module loading<br/>• Health checks"]
        DP["device-plugin<br/>━━━━━━━━━━━━<br/>• Registers hpe.com/cxi<br/>• Device allocation<br/>• NUMA awareness"]
        RH["retry-handler<br/>━━━━━━━━━━━━<br/>• cxi_rh supervisor<br/>• Per-device retry<br/>• Stats collection"]
        ME["metrics-exporter<br/>━━━━━━━━━━━━<br/>• Prometheus /metrics<br/>• Link state<br/>• Retry statistics"]
    end

    CXIDriver[("CXIDriver")] --> Operator
    Operator --> DaemonSets
```

### Reconciliation Flow

```mermaid
stateDiagram-v2
    [*] --> Fetch: CXIDriver event
    Fetch --> NotFound: Resource deleted
    Fetch --> CheckDeletion: Resource exists
    NotFound --> [*]

    CheckDeletion --> Cleanup: DeletionTimestamp set
    CheckDeletion --> SingletonCheck: Not deleting
    Cleanup --> [*]

    SingletonCheck --> Ignored: Another CXIDriver active
    SingletonCheck --> Reconcile: This is active driver
    Ignored --> UpdateStatus

    state Reconcile {
        [*] --> DriverAgent
        DriverAgent --> DevicePlugin: Success
        DevicePlugin --> RetryHandler: Success
        RetryHandler --> MetricsExporter: Success
        MetricsExporter --> [*]: Success

        DriverAgent --> Degraded: Failed
        DevicePlugin --> Degraded: Failed
        RetryHandler --> Degraded: Failed
        MetricsExporter --> Degraded: Failed
    }

    Reconcile --> UpdateStatus
    Degraded --> UpdateStatus
    UpdateStatus --> [*]: Set ObservedGeneration
```

## Prerequisites

- Kubernetes 1.26+
- Nodes with HPE Slingshot CXI hardware (PCI vendor ID `1590`)
- For DKMS driver installation: kernel headers installed on nodes
- Helm 3.x (for Helm installation) or kubectl with kustomize

## Installation

### Using Helm (Recommended)

```bash
# Add the chart repository (when published)
# helm repo add slingshot https://sielicki.github.io/slingshot-operator

# Or install from local chart
helm install slingshot-operator ./charts/slingshot-operator \
  --namespace slingshot-system \
  --create-namespace
```

### Using Kustomize

```bash
# Install CRDs
kubectl apply -k config/crd

# Deploy the operator
kubectl apply -k config/default
```

### Using Raw Manifests

```bash
kubectl apply -f https://raw.githubusercontent.com/sielicki/slingshot-operator/main/dist/install.yaml
```

## Configuration

After installing the operator, create a `CXIDriver` resource to configure driver management:

```yaml
apiVersion: cxi.hpe.com/v1
kind: CXIDriver
metadata:
  name: cxi-driver
spec:
  # Driver version to install
  version: "1.8.3"

  # Driver source configuration
  source:
    # Options: dkms, prebuilt, preinstalled
    type: preinstalled
    # For DKMS: URL to driver source archive
    # repository: "https://github.com/HewlettPackard/shs-cxi-driver/archive/refs/tags/v1.8.3.zip"

  # Retry handler configuration
  retryHandler:
    enabled: true
    # Options: daemonset, sidecar, kernel, none
    mode: daemonset
    image: ghcr.io/sielicki/slingshot-operator/retry-handler:latest

  # Kubernetes device plugin
  devicePlugin:
    enabled: true
    resourceName: "hpe.com/cxi"
    image: ghcr.io/sielicki/slingshot-operator/device-plugin:latest
    # Options: shared, exclusive
    sharingMode: shared
    # Max pods per device when sharingMode=shared
    sharedCapacity: 100

  # Prometheus metrics exporter
  metricsExporter:
    enabled: true
    image: ghcr.io/sielicki/slingshot-operator/metrics-exporter:latest
    port: 9090

  # Target only nodes with CXI hardware
  nodeSelector:
    feature.node.kubernetes.io/pci-1590.present: "true"

  # Update strategy for driver rollouts
  updateStrategy:
    type: RollingUpdate
    maxUnavailable: "10%"
    rollbackOnFailure: true
    healthCheckTimeout: "5m"
```

### Driver Source Types

| Type | Description | Use Case |
|------|-------------|----------|
| `preinstalled` | Expects driver already installed on host | Nodes with driver in base image |
| `dkms` | Downloads source and builds via DKMS | Flexible, works with any kernel |
| `prebuilt` | Downloads precompiled `.ko` files | Fast deployment, requires module cache |

```mermaid
flowchart LR
    subgraph DKMS["DKMS Installation"]
        direction TB
        D1["Download source<br/>(GitHub zip/tarball)"] --> D2["Extract & flatten"]
        D2 --> D3["Generate dkms.conf"]
        D3 --> D4["dkms add"]
        D4 --> D5["dkms build"]
        D5 --> D6["dkms install"]
        D6 --> D7["modprobe cxi_ss1"]
    end

    subgraph Prebuilt["Prebuilt Installation"]
        direction TB
        P1["Download .ko files"] --> P2["Copy to /lib/modules"]
        P2 --> P3["depmod -a"]
        P3 --> P4["modprobe cxi"]
    end

    subgraph Preinstalled["Preinstalled Verification"]
        direction TB
        V1["Check module loaded"] --> V2{"cxi_ss1 loaded?"}
        V2 -->|No| V3["modprobe cxi_ss1"]
        V2 -->|Yes| V4["Ready"]
        V3 --> V4
    end
```

### Retry Handler Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| `daemonset` | One retry handler pod per node | Default, simplest deployment |
| `sidecar` | Inject retry handler into application pods | Fine-grained control, pod-level isolation |
| `kernel` | Use kernel-space retry handling | Requires driver support |
| `none` | Disable retry handling | Testing or custom retry handling |

## CXIDevice Status

The operator automatically creates `CXIDevice` resources for each discovered device:

```bash
$ kubectl get cxidevices
NAME           NODE      DEVICE   NUMA   LINK   HEALTH    AGE
node1-cxi0     node1     cxi0     0      up     Healthy   5m
node1-cxi1     node1     cxi1     1      up     Healthy   5m
node2-cxi0     node2     cxi0     0      down   Unhealthy 5m
```

Detailed status:

```yaml
apiVersion: cxi.hpe.com/v1
kind: CXIDevice
metadata:
  name: node1-cxi0
status:
  node: node1
  device: cxi0
  pciAddress: "0000:41:00.0"
  numaNode: 0
  linkState: up
  driverVersion: "1.8.3"
  health:
    status: Healthy
    lastCheck: "2024-01-15T10:30:00Z"
  retryHandler:
    enabled: true
    policy: default
    stats:
      totalRetries: 1523
      nackRetries: 1200
      timeoutRetries: 323
      pendingRetries: 0
```

## Using CXI Devices in Pods

Request CXI devices in your pod spec:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-hpc-app
spec:
  containers:
  - name: app
    image: my-hpc-app:latest
    resources:
      limits:
        hpe.com/cxi: 1  # Request 1 CXI device
```

For sidecar retry handler mode, label your namespace:

```bash
kubectl label namespace my-namespace slingshot.hpe.com/inject-sidecar=enabled
```

## Metrics

The metrics exporter exposes Prometheus metrics at `/metrics`:

| Metric | Type | Description |
|--------|------|-------------|
| `cxi_device_link_state` | Gauge | Link state (1=up, 0=down) |
| `cxi_device_health_status` | Gauge | Health status (1=healthy, 0=unhealthy) |
| `cxi_retry_total` | Counter | Total retry operations |
| `cxi_retry_nack_total` | Counter | NACK-triggered retries |
| `cxi_retry_timeout_total` | Counter | Timeout-triggered retries |
| `cxi_driver_loaded` | Gauge | Driver module loaded (1=yes, 0=no) |

Example Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: 'cxi-metrics'
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
        action: keep
        regex: true
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_port]
        action: replace
        target_label: __address__
        regex: (.+)
        replacement: ${1}:9090
```

## Development

### Prerequisites

- Go 1.22+
- Docker or Podman
- Access to a Kubernetes cluster (kind, minikube, or remote)
- Nix (optional, for reproducible dev environment)

### Building

```bash
# Enter nix development shell (recommended)
nix-shell

# Or use system Go
make build

# Build all binaries
go build -o bin/manager ./cmd/main.go
go build -o bin/device-plugin ./cmd/device-plugin
go build -o bin/retry-handler ./cmd/retry-handler
go build -o bin/driver-agent ./cmd/driver-agent
go build -o bin/metrics-exporter ./cmd/metrics-exporter
```

### Testing

```bash
# Unit tests
make test

# E2E tests (requires kind)
make test-e2e

# Lint
make lint
```

### Regenerating Code

```bash
# Regenerate CRDs, RBAC, and DeepCopy methods
make manifests generate

# Sync CRDs to Helm chart
make helm-sync
```

### Building Images

```bash
# Build manager image
make docker-build IMG=ghcr.io/sielicki/slingshot-operator/manager:dev

# Build and push multi-arch
make docker-buildx IMG=ghcr.io/sielicki/slingshot-operator/manager:dev
```

## Project Structure

```
├── api/v1/                  # CRD type definitions
├── cmd/
│   ├── main.go              # Operator manager entrypoint
│   ├── device-plugin/       # Kubernetes device plugin
│   ├── driver-agent/        # Driver lifecycle agent
│   ├── metrics-exporter/    # Prometheus metrics
│   └── retry-handler/       # Retry handler supervisor
├── config/
│   ├── crd/                 # CRD manifests
│   ├── rbac/                # RBAC manifests
│   ├── manager/             # Manager deployment
│   └── samples/             # Example CRs
├── charts/slingshot-operator/  # Helm chart
├── internal/controller/     # Reconciliation logic
├── pkg/
│   ├── deviceplugin/        # Device plugin implementation
│   ├── driveragent/         # DKMS/module management
│   ├── metrics/             # Metrics collection
│   ├── retryhandler/        # cxi_rh wrapper
│   └── webhook/             # Sidecar injection webhook
└── test/e2e/                # E2E tests
```

## Troubleshooting

### Operator not creating DaemonSets

Check if a CXIDriver resource exists and the operator logs:

```bash
kubectl get cxidrivers
kubectl logs -n slingshot-system deployment/slingshot-operator
```

### Devices not discovered

Verify the driver is loaded and devices exist:

```bash
# On the node
ls /sys/class/cxi/
lsmod | grep cxi
```

### Multiple CXIDriver resources

Only one CXIDriver can be active. Additional resources will show `Degraded` condition:

```bash
kubectl get cxidrivers -o wide
kubectl describe cxidriver <name>
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
