# Spike S3 — CAPMOX Maturity (ClusterClass, arm64) and arm64 Images on Proxmox

- **Date:** 2026-10-30
- **Related:** ADR-0003 (Cluster API, RKE2 first), ADR-0007 (CAPI objects as source of truth), roadmap §3.2, §3.3, Phase 3b
- **Outcome:** 🟡 Desk result, lab run pending — **amd64 on Proxmox: viable** with a ClusterClass written by SEMAR. **arm64 on Proxmox: not available upstream** → fallback: arm64 node pools on OpenStack only until SEMAR owns an arm64 Packer template.

## Question

Is the Cluster API provider for Proxmox (CAPMOX, `ionos-cloud/cluster-api-provider-proxmox`) mature enough for Phase 3b — ClusterClass, RKE2, the CAPI v1beta2 contract, arm64 — and can image-builder produce arm64 node images on Proxmox VE?

## Findings

### 1. API and release state

| Item | State (2026-10) |
|---|---|
| Latest release | v0.9.1 (September 2026), CAPI 1.12.x |
| CAPMOX API | `v1alpha2` current, `v1alpha1` obsolete |
| CAPI contract | v1beta2 (with `v1alpha2`) |
| IPAM | CAPI in-cluster IPAM provider required (no DHCP) |
| Control plane endpoint | kube-vip on a mandatory `CONTROL_PLANE_ENDPOINT_IP` in the machines' subnet |
| Maintainer | IONOS; recent releases are dependency/bugfix roll-ups |

The CAPMOX API is still alpha. It follows the v1beta2 contract, so ADR-0007 (reading v1beta2 conditions from CAPI objects) holds, but CAPMOX's own types may change between minor versions.

### 2. ClusterClass and RKE2

- Upstream ClusterClass support is **experimental**, with `default`, `cilium` and `calico` flavors, all **kubeadm** based.
- RKE2 is not covered by CAPMOX docs or by Rancher's tests. Users report CAPMOX + CAPRKE2 working (via Rancher Turtles), but there is no upstream template.
- **Consequence:** SEMAR writes and versions its own ClusterClass for Proxmox: `ProxmoxClusterTemplate` + `ProxmoxMachineTemplate` + `RKE2ControlPlaneTemplate` + `RKE2ConfigTemplate`, with kube-vip as a static pod via RKE2 config and the VIP from a `NetworkPool` (in-cluster IPAM).

### 3. arm64

| Component | arm64 |
|---|---|
| CAPMOX controller image (`ghcr.io/ionos-cloud/cluster-api-provider-proxmox:v0.9.1`) | ❌ amd64 only (single-arch manifest; the Makefile has a multi-arch `docker-buildx` target that releases do not use) |
| CAPRKE2 control plane image v0.27.1 | ✅ amd64 + arm64 |
| CAPMOX support for arm64 guests (machine type, firmware) | not documented |
| image-builder Proxmox target | ❌ amd64 only: amd64 ISOs, goss `ARCH` fixed to `amd64`; OS variants Ubuntu 22.04/24.04/26.04 (BIOS and EFI), Rocky 9, Flatcar |

- Management cluster: CAPMOX must run on amd64 nodes, or SEMAR builds and hosts a multi-arch CAPMOX image (adds supply-chain work per release).
- arm64 guests on Proxmox need a PVE arm64 node (PVE 9.2+, roadmap §3.3), an aarch64 UEFI firmware, and a Packer template that SEMAR maintains. Building arm64 images under emulation is too slow (technical notes §7); they must be built on the arm64 node.

## Decisions (to be agreed)

1. Phase 3b keeps Proxmox **amd64** clusters with a SEMAR-owned RKE2 ClusterClass (CAPMOX `v1alpha2`, in-cluster IPAM, kube-vip).
2. Mixed amd64 + arm64 node pools are delivered on **OpenStack** first (already the Phase 3b exit criterion). arm64 on Proxmox moves behind a capability flag until a SEMAR arm64 Packer template has passed on a PVE arm64 node.
3. The management cluster runs CAPMOX on amd64 nodes; revisit if upstream publishes multi-arch images.
4. Pin CAPMOX in the compatibility matrix (`deploy/`) and treat each minor upgrade as a tested change, because the API is alpha.

## Open Items (lab)

- Create an amd64 RKE2 cluster on the Proxmox lab with a draft ClusterClass; scale, upgrade and delete it; record whether every step finished without manual work.
- Check that CAPMOX `v1alpha2` objects expose the v1beta2 conditions SEMAR reads (`Available`, `RollingOut`, `Remediating`).
- If a PVE arm64 node is available: build an Ubuntu 24.04 arm64 template with a custom Packer config and boot it through CAPMOX.
- Check arm64 images of the Proxmox CCM and CSI chosen for Phase 3b.
