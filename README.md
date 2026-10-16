# SEMAR

**System for Enterprise Multi-cloud & Application Runtime** — a single control plane for VMs on OpenStack and Proxmox VE, Kubernetes clusters (RKE2 via Cluster API), and unified observability, with x86_64 and ARM64 as first-class citizens.

> **Status:** Phase 1 (MVP) in progress. Not ready for production use.

## Progress

| Phase | Scope | State |
|---|---|---|
| 0 — Foundation | ADRs, CI, driver contract + Fake Driver, Temporal Lite profile (spike S4), web scaffold | Done, except the lab environment and spikes S1–S3 |
| 1 — MVP | Unified read-only inventory and console across OpenStack and Proxmox | In progress — done: PostgreSQL store, Settings API (sites, projects, providers, mappings), local auth + RBAC policies + hash-chained audit log, write-only provider credentials with KEK rotation, inventory persistence (conditional writes, orphan detection, change history, sync status), inventory API with project-scoped RBAC, capacity overview, authorization matrix in CI, 1,000-VM sync budget test; web sign-in, global Site/Project filter, Overview with capacity, Compute (VM list with filter chips, VM detail with history) and Settings (providers, sites, projects and mappings, write-only credentials). provider update and connection check (cloud identity, API versions), trusted origins for reverse proxies, read-only OpenStack and Proxmox drivers (tested against API fakes; lab verification pending). Operability: Prometheus metrics on a separate listener, correlation IDs to the audit log and to OpenStack, runbooks. Next: lab contract tests, console proxy |
| 2 and later | VM day-2 operations, OIDC, observability, Kubernetes lifecycle (Cluster API) | Not started |

## What's Here

| Component | Location | Notes |
|---|---|---|
| API server | `cmd/semar-api`, `internal/api` | Health/readiness/version, Settings, auth, credential and inventory endpoints; RFC 9457 errors; `X-Request-ID`; CSRF protection; 1 MiB body limit |
| Auth | `internal/auth`, `internal/api/auth.go` | Local users (argon2id), server-side sessions, role bindings, per-operation access policies (fail closed) |
| Generated API layer | `internal/api/gen` | Strict `net/http` server generated from the spec (`go generate ./...`) |
| Worker | `cmd/semar-worker`, `internal/workflow` | Temporal worker: per-provider sync Schedules reconciled with the registered providers, inventory persisted to PostgreSQL, AES-256-GCM payload codec with key rotation |
| OpenAPI contract | `api/openapi.yaml` | Source of truth for the frontend ↔ backend contract |
| Driver contract | `internal/driver/contract` | Provider-neutral model + read-only `CloudDriver` interface |
| Contract test suite | `internal/driver/contracttest` | Run by every driver on each PR (OpenStack and Proxmox against in-process API fakes); lab runs come next |
| OpenStack driver | `internal/driver/openstack` | Keystone v3 (application credential, or system-scoped reader user), Nova with per-call microversions, capacity from Placement, Glance, Neutron, Cinder; plain `net/http` |
| Proxmox driver | `internal/driver/proxmox` | API token, one `/cluster/resources` call, node failover, guest-agent IPs, TLS certificate pinning for self-signed clusters |
| Driver registry | `internal/driver/registry` | Builds a provider's driver from its discovery credential; shared by worker and API |
| Fake Driver | `internal/driver/fake` | Deterministic inventory + fault injection (latency, error rate, fail-next) |
| Web UI | `web/` | React + Vite + TypeScript + Tailwind; sign-in, global Site/Project filter, Overview (API status, capacity), Compute (VM list and detail), Settings (providers, sites, projects, mappings, credentials); module-based layout, API types generated from the spec (see `web/README.md`) |
| Store | `internal/store`, `internal/model`, `internal/secret` | PostgreSQL (pgx) with embedded goose migrations: sites, projects, providers, project mappings, users, sessions, role bindings, append-only hash-chained audit log, inventory cache with change history and sync status; credentials sealed with envelope encryption (ADR-0004) |
| Lite profile | `deploy/compose/` | PostgreSQL + Temporal + UI + SEMAR (development only) |
| ADRs & spikes | `docs/adr/`, `docs/spikes/` | ADR-0001 … ADR-0007, spike S4 |
| Runbooks | `docs/runbooks/` | Database roles, key rotation, break-glass, backup and restore |
| Metrics | `internal/metrics` | Prometheus text exposition for API and worker (no client library) |

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

The Lite profile does not serve the web UI yet. Run it from `web/` with `npm ci && npm run dev` and open http://localhost:5173; the dev server proxies the API on `127.0.0.1:8080`.

### Trying it out

With the stack running, sign in to the web UI as `admin` and open **Settings → Providers** to register a provider of type **Fake** (any endpoint URL, e.g. `https://fake.example.com`). Within one sync interval the worker creates its schedule and the first sync fills **Compute** and the capacity cards on **Overview**; open a VM for its detail and history. Add a project under **Settings → Projects** and map it to the fake scope `proj-a` to see ownership applied on the next sync, then pick the project in the global filter at the top of the page to narrow every view to it.

### API overview

The full contract is [`api/openapi.yaml`](api/openapi.yaml).

| Endpoint | Access |
|---|---|
| `GET /healthz`, `GET /readyz`, `GET /api/v1/version` | public |
| `POST /api/v1/auth/login`, `POST /api/v1/auth/logout` | public |
| `GET /api/v1/auth/me` | signed in |
| `GET /api/v1/sites`, `/projects`, `/projects/{id}/mappings`, `/providers`, `/providers/{id}` | signed in |
| `POST` sites, projects, mappings, providers; `PATCH`/`DELETE /providers/{id}` | admin |
| `POST /api/v1/providers/{id}/validate` | admin; connection check through the provider's driver |
| `GET/PUT/DELETE /api/v1/providers/{id}/credentials` | admin (secrets are write-only) |
| `GET /api/v1/resources?kind=vm\|hypervisor\|image\|network\|volume` (optional `providerId`, `siteId`, `projectId`, `arch`) | signed in; scoped to the caller's role bindings (see below) |
| `GET /api/v1/resources/{providerId}/{kind}/{externalId}` | signed in; same scoping, 404 outside it; includes orphaned resources and the latest 50 changes |
| `GET /api/v1/capacity` (optional `providerId`, `siteId`, `projectId`) | signed in; same scoping as `/resources`; physical capacity for global bindings only |
| `GET /api/v1/providers/{id}/sync-status` | signed in; providers in the caller's scope; `lastError` for global admins and operators only |

### Authentication and audit

Sign in with `POST /api/v1/auth/login`; the API sets an HttpOnly, SameSite=Strict session cookie. State-changing requests must carry an `X-Requested-By` header, and a browser `Origin` must match the request's host — or, behind a reverse proxy that rewrites `Host`, one of `SEMAR_TRUSTED_ORIGINS` (CSRF protection). Every operation in the spec has an access policy (public, authenticated or admin); operations without one are refused.

Logins, logouts and every Settings change are written to an append-only, hash-chained audit log. Verify it with:

```bash
semar-api audit-verify      # uses SEMAR_DATABASE_URL; exits non-zero if the chain is broken
```

Provider credentials are write-only: `PUT /api/v1/providers/{id}/credentials` seals the secret with envelope encryption and no endpoint ever returns it. To rotate the KEK, set a new `SEMAR_KEK_ID`/`SEMAR_KEK`, move the old key to `SEMAR_KEK_PREVIOUS`, restart, then run:

```bash
semar-api kek-rewrap        # rewraps every credential with the current KEK; then drop the old key
```

semar-worker never migrates: on start it waits until the migrations it needs are applied, and semar-api reports `not ready: schema` on `/readyz` while they are pending, so services can be restarted in any order during an upgrade.

In production run semar-api and semar-worker as a database role that is neither a superuser nor the owner of the tables, and apply migrations separately with `semar-api migrate` as the owner: the append-only guarantee relies on triggers that a superuser or the table owner could disable. See [docs/runbooks/database-roles.md](docs/runbooks/database-roles.md).

A lost local password is reset with `semar-api reset-password <username>` (new password on stdin; re-enables the account, ends its sessions, audited). See [docs/runbooks/break-glass.md](docs/runbooks/break-glass.md).

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
| `SEMAR_TRUSTED_ORIGINS` | — | Comma-separated public origins of the UI (`https://semar.example.com`) when a reverse proxy rewrites `Host`; replaces the Host comparison of the CSRF check |
| `SEMAR_HTTP_ADDR` | `:8080` | Listen address |
| `SEMAR_METRICS_ADDR` | — | Listen address of a separate `/metrics` listener (e.g. `127.0.0.1:9090`); off when empty |
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
| `SEMAR_DATABASE_URL` | — (required) | PostgreSQL URL; providers are read from and the inventory is written to it |
| `SEMAR_ORPHAN_AFTER` | `3` | Consecutive syncs a resource may be missing before it is marked orphaned |
| `SEMAR_SCHEDULE_RECONCILE_INTERVAL` | `1m` | How often sync schedules are reconciled with the registered providers |
| `SEMAR_METRICS_ADDR` | — | Listen address of `/metrics` and `/healthz` (e.g. `127.0.0.1:9091`); off when empty |
| `SEMAR_KEK_ID`, `SEMAR_KEK`, `SEMAR_KEK_PREVIOUS` | — | The same keys as semar-api; open discovery credentials inside sync activities (ADR-0005). Without them only `fake` providers sync |

### Inventory sync

The worker serves one site (`SEMAR_SITE_ID`; providers without a site belong to `default`). Every `SEMAR_SCHEDULE_RECONCILE_INTERVAL` it creates a Temporal Schedule for each active provider of its site and deletes schedules of removed providers. Each sync lists the provider's full inventory and applies it in one transaction:

- snapshots older than the last applied one are discarded;
- resources leased by an in-flight operation are left untouched;
- project ownership is re-derived from project mappings;
- a resource missing for `SEMAR_ORPHAN_AFTER` consecutive syncs is marked orphaned (never deleted) and restored if it reappears;
- every create, change, orphaning and restore is appended to `resource_changes`.

### Provider credentials

OpenStack and Proxmox providers need a **discovery** credential (purpose `discovery`, scope `provider`) before they sync. The worker decrypts it inside the sync activity; it never enters a Temporal payload. A provider without one shows the reason in its sync status and is skipped until it is stored.

| Provider | Secret fields | Grants needed for read-only inventory |
|---|---|---|
| OpenStack | `application_credential_id`, `application_credential_secret` — lists the credential's own project | `reader` on the project |
| OpenStack (all projects) | `username`, `password`, optional `user_domain_name` (default `Default`) — system-scoped token | `reader` with system scope |
| Proxmox VE | `token_id` (`user@realm!name`), `token_secret` | `PVEAuditor` on `/` |

Self-signed Proxmox clusters are trusted by pinning the certificate's SHA-256 fingerprint (`tlsFingerprint` on the provider, from `pvenode cert info`); the pin is checked on every connection, including resumed TLS sessions.

The drivers are verified against in-process fakes of the provider APIs (`go test ./internal/driver/...`); verification against real OpenStack and Proxmox labs is still pending.

### Changing a provider

A provider is updated in place (`PATCH /api/v1/providers/{id}`, or **Edit** on its Settings page), never registered twice: ID and type are fixed, the endpoint fingerprint is re-derived, and a provider cannot be pointed at another provider's cloud. Changing endpoints or region clears the last connection check. Moving a provider to another site moves its sync schedule onto that site's worker within one `SEMAR_SCHEDULE_RECONCILE_INTERVAL`.

`POST /api/v1/providers/{id}/validate` (**Check connection**) asks the driver for the cloud's identity and maximum API versions and records them. Two providers can never share a cloud identity, which also catches one cloud registered under two URLs. Driver errors are logged; clients only see a classification (refused credentials, unreachable, timeout).

`GET /api/v1/resources` serves the cache, never the providers, and applies RBAC in the query: global admins and operators see every resource (including those not mapped to a project), global viewers see every resource that belongs to a project, and project- or provider-scoped bindings see only their scope. Hypervisors belong to no project and are visible to global bindings only. `GET /api/v1/capacity` aggregates through the same predicate, so totals never include resources the caller could not list; orphaned resources are not counted. An authorization matrix test exercises every operation against every kind of caller and fails when a new operation has no row.

## Operations

**Metrics.** With `SEMAR_METRICS_ADDR` set, each process serves Prometheus metrics on its own listener, so they can stay on an internal interface:

| Metric | Process | Labels |
|---|---|---|
| `semar_http_requests_total`, `semar_http_request_duration_seconds` | semar-api | `method`, `route` (the route pattern, never raw paths), `status` |
| `semar_inventory_syncs_total` | semar-worker | `provider`, `outcome` (`success`, `failure`, `skipped`, `stale`) |
| `semar_inventory_sync_duration_seconds`, `semar_inventory_last_success_timestamp_seconds`, `semar_inventory_vms` | semar-worker | `provider` |
| `semar_build_info`, `go_goroutines`, `go_memstats_heap_alloc_bytes`, `process_start_time_seconds` | both | |

**Correlation.** Every API request has an `X-Request-ID` (accepted from the client when well formed, otherwise generated). It is returned in the response and in problem bodies, written to access logs and to every audit event, and sent to OpenStack as `X-OpenStack-Request-ID`. Syncs use the Temporal run ID the same way, so a sync can be followed from Temporal into the provider's logs.

**Runbooks** live in [docs/runbooks/](docs/runbooks/).

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

## Architecture Decisions

Binding architecture decisions are recorded as ADRs in [`docs/adr/`](docs/adr/); spike reports live in [`docs/spikes/`](docs/spikes/).
