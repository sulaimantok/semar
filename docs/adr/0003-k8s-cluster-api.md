# ADR-0003: Lifecycle Kubernetes — Cluster API + ClusterClass

- **Status:** Accepted
- **Tanggal:** 2026-10-12

## Konteks
SEMAR harus membuat dan mengelola cluster Kubernetes di OpenStack dan Proxmox, termasuk day-2 (scale, upgrade, remediation, delete bersih). Membangun reconciliation sendiri tidak realistis untuk tim 1–2 orang.

## Keputusan
- Gunakan **Cluster API** dengan **ClusterClass (managed topology)**. SEMAR hanya mengubah `Cluster.spec.topology`.
- Provider: **CAPO** (OpenStack), **CAPMOX** (Proxmox), **CAPRKE2** (bootstrap + control plane), **CAAPH** (add-on Helm).
- Distribusi pertama hanya **RKE2**. Kubeadm/KCP masuk backlog.
- Klien ditulis terhadap **CAPI v1beta2**.
- Satu cluster yang dibuat SEMAR = satu provider.
- Provisioner cloud-init/SSH buatan sendiri **tidak** dibangun.

## Konsekuensi
- Day-2 (rolling upgrade, `MachineHealthCheck`, scale) tersedia dari ekosistem.
- Management cluster (RKE2 3-node, dibangun di luar SEMAR) menjadi **aset Tier-0** dengan backup (etcd snapshot + `clusterctl move --to-directory`) dan restore drill.
- Beban upgrade per kuartal dengan matriks kompatibilitas terkunci.
- CAPMOX masih ber-API alpha dan dikelola vendor (IONOS); Proxmox dijadwalkan di Phase 3b setelah spike.
- CCM/CSI meninggalkan resource (LB, volume) yang tidak dihapus CAPI, sehingga delete memerlukan saga pre-delete + sweep.

## Alternatif
- **Orchestrator cloud-init buatan sendiri:** cepat untuk create, tetapi tidak menyelesaikan day-2.
- **Rancher:** matang, tetapi menambah control plane terpisah dan tidak menyatu dengan model VM/Project SEMAR.
