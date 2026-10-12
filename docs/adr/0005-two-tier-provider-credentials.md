# ADR-0005: Two-Tier Provider Credentials

- **Status:** Accepted
- **Date:** 2026-10-12

## Context
Inventory discovery needs visibility across projects (hypervisors, Placement, resources not yet mapped), while least privilege requires mutations to be scoped per Project. OpenStack Application Credentials are always project-scoped and belong to the user who created them; a Proxmox token scoped to one pool cannot see nodes or storage outside that pool.

## Decision
A `ProviderCredential` entity with a `purpose`:

| Purpose | OpenStack | Proxmox | Count |
|---|---|---|---|
| `discovery` | Service user with the `reader` role (system/domain scope) | Cluster-wide `PVEAuditor` token | 1 per provider |
| `mutate` | Application Credential per project | Per-pool token + custom role | 1 per ProjectMapping |
| `capi` | Dedicated Application Credential for CAPO | Dedicated token for CAPMOX | 1 per Project |
| `cluster` | Restricted Application Credential for CCM/CSI | Restricted token for CCM/CSI | 1 per workload cluster |

## Consequences
- Full discovery without granting broad mutation rights.
- Application Credentials are created by a **dedicated service user**; rotation = create new → swap → delete old, and must propagate to CAPI identity Secrets.
- `cluster` credentials are readable by a workload cluster's cluster-admin, so their scope is minimal and they are rotated when the cluster is deleted.
- The Proxmox ACL list is documented in a runbook and exercised by the mutating contract tests.

## Alternatives
- **One admin credential per provider:** simple, but a leak means full access.
- **Per-project credentials only:** cannot see hypervisors, capacity, or `unassigned` resources.
