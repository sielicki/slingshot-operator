# Contributing to Slingshot Operator

## Development Setup

### Prerequisites

- Go 1.24+
- Docker or Podman
- kubectl
- kind (for local testing)
- Nix (optional, for reproducible dev environment)

### Quick Start with Nix

```bash
nix-shell
go build ./...
make test
```

### Without Nix

```bash
# Install dependencies
go mod download

# Build
go build ./...

# Run tests
make test
```

## Making Changes

### Code Generation

After modifying API types in `api/v1/`, regenerate code:

```bash
make manifests generate
```

### Running Locally

```bash
# Install CRDs
make install

# Run controller locally
make run
```

### Testing

```bash
# Unit tests
make test

# E2E tests (requires kind)
make test-e2e
```

## Pull Request Process

1. Fork the repository
2. Create a feature branch from `main`
3. Make your changes
4. Ensure tests pass: `make test`
5. Ensure linter passes: `make lint`
6. Ensure generated code is up to date: `make manifests generate`
7. Submit a pull request

### Commit Messages

Use conventional commit format:

- `feat:` New features
- `fix:` Bug fixes
- `docs:` Documentation changes
- `chore:` Maintenance tasks
- `refactor:` Code refactoring
- `test:` Test additions/changes

### PR Requirements

- All CI checks must pass
- Code must be reviewed by a maintainer
- Documentation must be updated for user-facing changes

## Release Process

Releases are automated via GitHub Actions when a tag is pushed:

```bash
git tag v1.0.0
git push origin v1.0.0
```

This will:
1. Build container images for all components
2. Push to ghcr.io
3. Create a GitHub release with install manifests

## Architecture

```
├── api/v1/              # CRD type definitions
├── cmd/                 # Binary entrypoints
│   ├── main.go         # Operator manager
│   ├── device-plugin/  # Kubernetes device plugin
│   ├── driver-agent/   # Driver lifecycle manager
│   └── retry-handler/  # cxi_rh supervisor
├── config/             # Kubernetes manifests (kustomize)
├── internal/controller/ # Reconciliation logic
└── pkg/                # Shared packages
    ├── deviceplugin/   # Device plugin implementation
    ├── driveragent/    # Driver management
    ├── retryhandler/   # Retry handler supervisor
    └── webhook/        # Sidecar injection webhook
```

## Getting Help

- Open an issue for bugs or feature requests
- Check existing issues before creating new ones
