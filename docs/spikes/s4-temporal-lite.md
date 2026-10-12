# Spike S4 — Temporal Schedules + PayloadCodec on the Lite Profile

- **Date:** 2026-10-12
- **Related:** ADR-0002 (Temporal), ADR-0004 (secrets), roadmap §2.1 (deployment profiles), §3.6 (inventory sync), §3.8 (workflow conventions)
- **Outcome:** ✅ Success — adopt as the Phase 1 baseline.

## Question

Can the Lite profile (one PostgreSQL, Temporal, `semar-api`, `semar-worker`) run per-provider inventory sync as Temporal Schedules, with every payload encrypted, on a small ARM64 host — and is it simple enough for a 1–2 person team to operate?

## Setup

- Host: ARM64 VM, Ubuntu 24.04, 4 vCPU, 7.7 GiB RAM, Docker 29 + Compose 2.40.
- Stack: `deploy/compose/compose.lite.yaml` — PostgreSQL 16, Temporal server/admin-tools 1.32.1, Temporal UI 2.55.0, `semar-worker` and `semar-api` from the repo image.
- Worker: Temporal Go SDK v1.49.0, Fake Driver provider `fake-1`, sync interval 15 s for the test (default 60 s).

## Findings

### 1. Temporal deployment

- `temporalio/auto-setup` is no longer published for current versions, and the official `temporalio/docker-compose` repository still pins 1.29 with auto-setup.
- `temporalio/server:1.32.1` requires a config file. We ship `deploy/compose/temporal/config.yaml`, rendered by the server from environment variables (`# enable-template`).
- Schema creation and migration run as a one-shot `temporal-schema` container (`temporal-sql-tool`); namespace registration runs in `temporal-namespace`. Both are **idempotent**: re-running `docker compose up` exits 0 with "schema up to date" / "namespace exists".
- First start from pulled images to a running stack: ~56 s (including image build).

### 2. Schedules

| Check | Result |
|---|---|
| Schedule `sync-<provider>` created on worker start | ✅ |
| Overlap policy | `SCHEDULE_OVERLAP_POLICY_SKIP` (singleton sync) |
| Runs on the per-site task queue | ✅ `site-default` |
| Fires on interval | ✅ every 15 s, workflow completes in milliseconds with the Fake Driver |
| Worker restart takes the "already exists → update" path | ✅ no error |

SKIP behaviour under a long-running sync was **not** exercised empirically (Fake syncs finish in milliseconds); it relies on Temporal semantics. Revisit with a real provider in the lab.

### 3. Payload encryption

| Check | Result |
|---|---|
| History blobs in PostgreSQL containing plaintext JSON (`"ProviderID"`) | **0 of 41** |
| History blobs carrying the encrypted encoding | 20 of 41 (the others have no payload) |
| Schedule action input | encrypted (`binary/encrypted-aes256gcm`) |
| Key rotation k1 → k2 (k1 kept as previous key) | ✅ syncs continue; schedule input re-encrypted with k2 on worker start |
| Second rotation k2 → k3 | ✅ |

Rotation note: Schedule action arguments are encrypted when the schedule is written. `EnsureSyncSchedule` rewrites the schedule on every worker start, so after a rotation the action is re-encrypted with the new key. Old keys must stay in `SEMAR_PAYLOAD_PREVIOUS_KEYS` until no open workflow or schedule still uses them.

### 4. Determinism guard

- `internal/workflow/testdata/sync_provider_history.json` is a history recorded from this stack (encrypted with the public test key `k2`, see `replay_test.go`).
- `TestReplaySyncProviderHistory` passes on the current code and **fails** (`TMPRL1100`) when a non-deterministic change (an extra `workflow.Sleep`) is introduced — confirming the replay test works as a CI guard.

### 5. Resource footprint (idle, steady state)

| Container | Memory |
|---|---|
| postgres | 131 MiB |
| temporal (all services, 4 history shards) | 94 MiB |
| semar-worker | 12.5 MiB |
| temporal-ui | 6.4 MiB |
| semar-api | 1.8 MiB |
| **Total** | **≈ 246 MiB** |

## Decisions

1. Keep Temporal from Phase 1 (ADR-0002 confirmed); run Temporal with our own config file instead of `auto-setup`.
2. Pin Temporal server/admin-tools 1.32.1 and UI 2.55.0 in the compose file; upgrades follow the quarterly upgrade budget.
3. Payload keys: `SEMAR_PAYLOAD_KEY_ID` + `SEMAR_PAYLOAD_KEY` + `SEMAR_PAYLOAD_PREVIOUS_KEYS` for now; move to `SecretStore` when it lands in Phase 1.
4. The replay test is mandatory for every workflow; refresh its recording only together with a versioned change.

## Open Items

- Exercise overlap SKIP and retries against a slow/failing real provider in the lab.
- mTLS between workers and the Temporal frontend, and restricting the Temporal UI (roadmap §3.4) — required before any shared deployment.
- Persist sync results into the SEMAR inventory cache (Phase 1 store).
