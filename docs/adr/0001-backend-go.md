# ADR-0001: Backend Language — Go

- **Status:** Accepted
- **Date:** 2026-10-12

## Context
SEMAR talks to OpenStack, Proxmox VE, Kubernetes/Cluster API and Temporal, and must run on `linux/amd64` and `linux/arm64`. The team is 1–2 engineers, some of whom are used to Python (FastAPI).

## Decision
The whole core backend (`semar-api`, `semar-worker`) is written in **Go**. No second language in the core backend.

## Consequences
- Native integrations: `gophercloud`, `client-go`/`controller-runtime` (CAPI v1beta2), Temporal Go SDK.
- One static binary per architecture; small distroless images; cross-compilation without emulation.
- Learning curve for team members used to Python; code conventions and linting (`golangci-lint`) are needed from day one.

## Alternatives
- **Python (FastAPI):** fast for an MVP and familiar, but CAPI/controller-runtime and Temporal integration is less native and multi-arch distribution is heavier.
