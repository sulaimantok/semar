# SEMAR

**System for Enterprise Multi-cloud & Application Runtime** — a single control plane for VMs on OpenStack and Proxmox VE, Kubernetes clusters (RKE2 via Cluster API), and unified observability, with x86_64 and ARM64 as first-class citizens.

> **Status:** Phase 1 (MVP) in progress. Not ready for production use.

## Progress

| Phase | Scope | State |
|---|---|---|
| 0 — Foundation | ADRs, CI, driver contract + Fake Driver, Temporal Lite profile (spike S4), web scaffold | Done, except the lab environment and spikes S1–S3 |
| 1 — MVP | Unified read-only inventory and console across OpenStack and Proxmox | In progress — done: PostgreSQL store, Settings API (sites, projects, providers, mappings), local auth + RBAC policies + hash-chained audit log, write-only provider credentials with KEK rotation. Next: inventory persistence, OpenStack/Proxmox drivers, console proxy, UI pages |
| 2 and later | VM day-2 operations, OIDC, observability, Kubernetes lifecycle (Cluster API) | Not started |

Roadmap, detailed tracker and exit criteria are kept in the planning workspace (see [Planning Documents](#planning-documents)).

## What's Here

| Component | Location | Notes |
|---|---|---|
| API server | `cmd/semar-api`, `internal/api` | Health/readiness/version, Settings, auth and credential endpoints; RFC 9457 errors; `X-Request-ID`; CSRF protection; 1 MiB body limit |
| Auth | `internal/auth`, `internal/api/auth.go` | Local users (argon2id), server-side sessions, role bindings, per-operation access policies (fail closed) |
| Generated API layer | `internal/api/gen` | Strict `net/http` server generated from the spec (`go generate ./...`) |
| Worker | `cmd/semar-worker`, `internal/workflow` | Temporal worker: per-provider sync Schedules, AES-256-GCM payload codec with key rotation |
| OpenAPI contract | `api/openapi.yaml` | Source of truth for the frontend ↔ backend contract |
| Driver contract | `internal/driver/contract` | Provider-neutral model + read-only `CloudDriver` interface |
| Contract test suite | `internal/driver/contracttest` | Run by every driver (Fake on each PR, lab nightly) |
| Fake Driver | `internal/driver/fake` | Deterministic inventory + fault injection (latency, error rate, fail-next) |
| Web UI | `web/` | React + Vite + TypeScript + Tailwind; module-based layout, API types generated from the spec (see `web/README.md`) |
| Store | `internal/store`, `internal/model`, `internal/secret` | PostgreSQL (pgx) with embedded goose migrations: sites, projects, providers, project mappings, users, sessions, role bindings, append-only hash-chained audit log; credentials sealed with envelope encryption (ADR-0004) |
| Lite profile | `deploy/compose/` | PostgreSQL + Temporal + UI + SEMAR (development only) |
| ADRs & spikes | `docs/adr/`, `docs/spikes/` | ADR-0001 … ADR-0007, spike S4 |

## Development

Prerequisites: Go 1.26+, `golangci-lint` v2, Docker Buildx (optional).

```bash
make test      # go test -race (database tests need SEMAR_TEST_DATABASE_URL, see below)
make lint      # golangci-lint
make generate  # regenerate internal/api/gen from api/openapi.yaml
make build     # bin/semar-api, bin/semar-worker
make run-api   # listens on :8080 (needs SEMAR_DATABASE_URL)
make image     # multi-arch image for linux/amd64 + linux/arm64
```

Web UI (Node 24+): `cd web && npm ci && npm run dev`, then open http://localhost:5173 (proxies the API on `:8080`).

Database tests create and drop a throwaway database per test. Point them at a PostgreSQL user that may create databases (they are skipped locally when unset and fail in CI):

```bash
export SEMAR_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable
```

### Lite profile (local stack)

```bash
cp deploy/compose/.env.example deploy/compose/.env
# set POSTGRES_PASSWORD, SEMAR_PAYLOAD_KEY, SEMAR_KEK and SEMAR_BOOTSTRAP_ADMIN_PASSWORD
# (keys: `openssl rand -base64 32`)
make lite-up      # postgres, temporal, temporal-ui, semar-worker, semar-api
make lite-logs    # follow the worker
```

Then sign in as `admin` with the bootstrap password and remove `SEMAR_BOOTSTRAP_ADMIN_PASSWORD` from `.env`:

```bash
curl -c jar -H 'X-Requested-By: cli' -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<bootstrap password>"}' http://127.0.0.1:8080/api/v1/auth/login
curl -b jar http://127.0.0.1:8080/api/v1/providers
```

Ports are bound to `127.0.0.1`: API `8080`, Temporal `7233`, Temporal UI `8233`, PostgreSQL `5432`. See [spike S4](docs/spikes/s4-temporal-lite.md) for measurements.

### API overview

The full contract is [`api/openapi.yaml`](api/openapi.yaml).

| Endpoint | Access |
|---|---|
| `GET /healthz`, `GET /readyz`, `GET /api/v1/version` | public |
| `POST /api/v1/auth/login`, `POST /api/v1/auth/logout` | public |
| `GET /api/v1/auth/me` | signed in |
| `GET /api/v1/sites`, `/projects`, `/projects/{id}/mappings`, `/providers`, `/providers/{id}` | signed in |
| `POST` sites, projects, mappings, providers; `DELETE /providers/{id}` | admin |
| `GET/PUT/DELETE /api/v1/providers/{id}/credentials` | admin (secrets are write-only) |

### Authentication and audit

Sign in with `POST /api/v1/auth/login`; the API sets an HttpOnly, SameSite=Strict session cookie. State-changing requests must carry an `X-Requested-By` header (CSRF protection). Every operation in the spec has an access policy (public, authenticated or admin); operations without one are refused.

Logins, logouts and every Settings change are written to an append-only, hash-chained audit log. Verify it with:

```bash
semar-api audit-verify      # uses SEMAR_DATABASE_URL; exits non-zero if the chain is broken
```

Provider credentials are write-only: `PUT /api/v1/providers/{id}/credentials` seals the secret with envelope encryption and no endpoint ever returns it. To rotate the KEK, set a new `SEMAR_KEK_ID`/`SEMAR_KEK`, move the old key to `SEMAR_KEK_PREVIOUS`, restart, then run:

```bash
semar-api kek-rewrap        # rewraps every credential with the current KEK; then drop the old key
```

In production run semar-api as a non-superuser database role: the append-only guarantee relies on triggers that a superuser could disable.

### Configuration

`semar-api` is configured through environment variables:

| Variable | Default | Description |
|---|---|---|
| `SEMAR_DATABASE_URL` | — (required) | PostgreSQL URL, e.g. `postgres://semar:…@localhost:5432/semar?sslmode=disable` |
| `SEMAR_MIGRATE_ON_START` | `true` | Apply pending migrations before serving (safe with several replicas) |
| `SEMAR_BOOTSTRAP_ADMIN_PASSWORD` | — | Creates the `admin` user (global Admin) on first start when no user exists; remove afterwards |
| `SEMAR_SESSION_TTL` | `12h` | Login session lifetime (1m–168h) |
| `SEMAR_COOKIE_SECURE` | `true` | Secure flag of the session cookie; set `false` only for plain HTTP on localhost |
| `SEMAR_KEK_ID` | `k1` | ID of the current key-encryption key for provider credentials |
| `SEMAR_KEK` | — (required) | Current KEK, 32 bytes base64 (`openssl rand -base64 32`); keep it outside the database host in production |
| `SEMAR_KEK_PREVIOUS` | — | Old KEKs kept for decryption during rotation: `k0=<base64>,…` |
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
