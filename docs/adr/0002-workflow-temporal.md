# ADR-0002: Workflow Engine — Temporal

- **Status:** Accepted
- **Date:** 2026-10-12

## Context
VM provisioning and cluster operations are long-running and prone to partial failure (network timeouts, slow provider APIs, worker restarts). Inventory sync per provider must run as a singleton with backoff.

## Decision
Use **Temporal** from Phase 1:
- **Schedules** per provider for inventory sync (overlap policy SKIP).
- **Workflows** for every VM and cluster mutation, following the idempotency conventions (roadmap §3.8).
- **One task queue per site** (`site-<id>`) so workers can later run at remote sites.

## Consequences
- Retries, timeouts, compensation (Saga) and restart resilience come built in, with no hand-written state machine.
- Activities **must be idempotent** (find-or-create tagged with `semar.op_id`), because a retry can happen after the provider call has already succeeded.
- Temporal payloads and persistence are a sensitive surface: no secrets in payloads, a PayloadCodec encrypts payloads, mTLS to the frontend, and the UI is Admin-only.
- Workflow code must be versioned and covered by replay tests in CI.
- One more component to operate; the Lite profile reuses the same PostgreSQL instance (separate database) with SQL visibility.

## Alternatives
- **Asynq/Celery + a state machine in PostgreSQL:** lighter, but retries, compensation and singletons would have to be built by hand and are bug-prone.
- **Goroutine + ticker for sync:** enough for read-only work, but provides neither a singleton across replicas nor per-site task queues.
