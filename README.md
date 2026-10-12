# SEMAR

**System for Enterprise Multi-cloud & Application Runtime** — a single control plane for VMs on OpenStack and Proxmox VE, Kubernetes clusters (RKE2 via Cluster API), and unified observability, with x86_64 and ARM64 as first-class citizens.

> **Status:** Phase 0 (foundation). Not ready for use.

## What's Here

| Component | Location | Notes |
|---|---|---|
| API server | `cmd/semar-api`, `internal/api` | `/healthz`, `/readyz`, `/api/v1/version`; RFC 9457 errors; `X-Request-ID` |
| Generated API layer | `internal/api/gen` | Strict `net/http` server generated from the spec (`go generate ./...`) |
| Worker | `cmd/semar-worker`, `internal/workflow` | Temporal worker: per-provider sync Schedules, AES-256-GCM payload codec with key rotation |
| OpenAPI contract | `api/openapi.yaml` | Source of truth for the frontend ↔ backend contract |
| Driver contract | `internal/driver/contract` | Provider-neutral model + read-only `CloudDriver` interface |
| Contract test suite | `internal/driver/contracttest` | Run by every driver (Fake on each PR, lab nightly) |
| Fake Driver | `internal/driver/fake` | Deterministic inventory + fault injection (latency, error rate, fail-next) |
| Lite profile | `deploy/compose/` | PostgreSQL + Temporal + UI + SEMAR (development only) |
| ADRs & spikes | `docs/adr/`, `docs/spikes/` | ADR-0001 … ADR-0007, spike S4 |

## Development

Prerequisites: Go 1.26+, `golangci-lint` v2, Docker Buildx (optional).

```bash
make test      # go test -race
make lint      # golangci-lint
make generate  # regenerate internal/api/gen from api/openapi.yaml
make build     # bin/semar-api, bin/semar-worker
make run-api   # listens on :8080
make image     # multi-arch image for linux/amd64 + linux/arm64
```

### Lite profile (local stack)

```bash
cp deploy/compose/.env.example deploy/compose/.env   # set POSTGRES_PASSWORD and SEMAR_PAYLOAD_KEY
make lite-up                                         # postgres, temporal, temporal-ui, semar-worker, semar-api
make lite-logs                                       # follow the worker
```

Ports are bound to `127.0.0.1`: API `8080`, Temporal `7233`, Temporal UI `8233`, PostgreSQL `5432`. See [spike S4](docs/spikes/s4-temporal-lite.md) for measurements.

### Configuration

`semar-api` is configured through environment variables:

| Variable | Default | Description |
|---|---|---|
| `SEMAR_HTTP_ADDR` | `:8080` | Listen address |
| `SEMAR_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `SEMAR_SHUTDOWN_TIMEOUT` | `15s` | Graceful shutdown limit |

`semar-worker`:

| Variable | Default | Description |
|---|---|---|
| `SEMAR_TEMPORAL_ADDRESS` | `localhost:7233` | Temporal frontend |
| `SEMAR_TEMPORAL_NAMESPACE` | `semar` | Temporal namespace |
| `SEMAR_SITE_ID` | `default` | Site served by this worker (task queue `site-<id>`) |
| `SEMAR_SYNC_INTERVAL` | `60s` | Inventory sync interval per provider |
| `SEMAR_PAYLOAD_KEY_ID` | `k1` | ID of the current payload key |
| `SEMAR_PAYLOAD_KEY` | — (required) | 32-byte AES key, base64 (`openssl rand -base64 32`) |
| `SEMAR_PAYLOAD_PREVIOUS_KEYS` | — | Keys kept for decryption during rotation: `k0=<base64>,…` |
| `SEMAR_FAKE_PROVIDERS` | `fake-1` | Fake Driver providers to sync (until providers come from the database) |

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
