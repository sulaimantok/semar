# SEMAR

**System for Enterprise Multi-cloud & Application Runtime** — a single control plane for VMs on OpenStack and Proxmox VE, Kubernetes clusters (RKE2 via Cluster API), and unified observability, with x86_64 and ARM64 as first-class citizens.

> **Status:** Phase 0 (foundation). Not ready for use.

## What's Here

| Component | Location | Notes |
|---|---|---|
| API server | `cmd/semar-api`, `internal/api` | `/healthz`, `/readyz`, `/api/v1/version`; RFC 9457 errors; `X-Request-ID` |
| OpenAPI contract | `api/openapi.yaml` | Source of truth for the frontend ↔ backend contract |
| Driver contract | `internal/driver/contract` | Provider-neutral model + read-only `CloudDriver` interface |
| Contract test suite | `internal/driver/contracttest` | Run by every driver (Fake on each PR, lab nightly) |
| Fake Driver | `internal/driver/fake` | Deterministic inventory + fault injection (latency, error rate, fail-next) |
| ADRs | `docs/adr/` | ADR-0001 … ADR-0007 |

## Development

Prerequisites: Go 1.26+, `golangci-lint` v2, Docker Buildx (optional).

```bash
make test      # go test -race
make lint      # golangci-lint
make build     # bin/semar-api
make run-api   # listens on :8080
make image     # multi-arch image for linux/amd64 + linux/arm64
```

`semar-api` is configured through environment variables:

| Variable | Default | Description |
|---|---|---|
| `SEMAR_HTTP_ADDR` | `:8080` | Listen address |
| `SEMAR_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `SEMAR_SHUTDOWN_TIMEOUT` | `15s` | Graceful shutdown limit |

## Adding a Provider Driver

1. Implement `contract.CloudDriver` (plus optional interfaces such as `contract.IncrementalVMLister` where supported).
2. Wrap provider errors with the sentinels in `contract` (`ErrUnavailable`, `ErrUnauthorized`, …).
3. Run the shared suite from the driver's tests:

```go
func TestReadOnlyContract(t *testing.T) {
	contracttest.RunReadOnly(t, func(t *testing.T) contract.CloudDriver {
		return mydriver.New(...)
	})
}
```

## Planning Documents

The roadmap, implementation tracker and technical notes are maintained in the planning workspace (`tech-garden-dev/opensources/recommendation/semar/`). Binding architecture decisions live in [`docs/adr/`](docs/adr/).
