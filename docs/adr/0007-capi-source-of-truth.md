# ADR-0007: CAPI Objects as the Source of Truth for Cluster State

- **Status:** Accepted
- **Date:** 2026-10-12

## Context
Cluster state can change outside SEMAR (CAPI reconciliation, `kubectl` against the management cluster, automatic remediation). Keeping cluster state as Temporal workflow state would drift from reality.

## Decision
- **CAPI objects in the management cluster are the source of truth.** SEMAR's database is only a cache.
- Temporal workflows only **submit topology changes** and then wait on v1beta2 conditions (`Available`, `RollingOut`, `ScalingUp/Down`, `Remediating`) with timeouts.
- UI state is **derived from conditions**, not from workflow state.
- VMs owned by a CAPI cluster are read-only in the VM Manager.

## Consequences
- No conflict between workflows and reconciliation.
- SEMAR needs to watch/poll CAPI objects and map conditions to UI state.
- Losing SEMAR's database does not lose cluster state; the cache can be rebuilt from the management cluster.

## Alternatives
- **A cluster state machine in SEMAR's workflows/database:** prone to drift and duplicates CAPI logic.
