# ADR-0003: Kubernetes Lifecycle — Cluster API + ClusterClass

- **Status:** Accepted
- **Date:** 2026-10-12

## Context
SEMAR must create and manage Kubernetes clusters on OpenStack and Proxmox, including day-2 operations (scale, upgrade, remediation, clean delete). Building our own reconciliation is not realistic for a team of 1–2.

## Decision
- Use **Cluster API** with **ClusterClass (managed topology)**. SEMAR only changes `Cluster.spec.topology`.
- Providers: **CAPO** (OpenStack), **CAPMOX** (Proxmox), **CAPRKE2** (bootstrap + control plane), **CAAPH** (Helm add-ons).
- The first distribution is **RKE2** only. Kubeadm/KCP goes to the backlog.
- The client targets **CAPI v1beta2**.
- One SEMAR-provisioned cluster = one provider.
- No home-grown cloud-init/SSH provisioner.

## Consequences
- Day-2 (rolling upgrade, `MachineHealthCheck`, scaling) comes from the ecosystem.
- The management cluster (3-node RKE2, built outside SEMAR) becomes a **Tier-0 asset** with backups (etcd snapshot + `clusterctl move --to-directory`) and restore drills.
- A quarterly upgrade budget against a pinned compatibility matrix.
- CAPMOX still has an alpha API and is vendor-maintained (IONOS); Proxmox is scheduled for Phase 3b after a spike.
- CCM/CSI leave behind resources (load balancers, volumes) that CAPI does not delete, so deletion needs a pre-delete saga plus a sweep.

## Alternatives
- **Home-grown cloud-init orchestrator:** quick for create, but does not solve day-2.
- **Rancher:** mature, but adds a separate control plane that does not fit SEMAR's VM/Project model.
