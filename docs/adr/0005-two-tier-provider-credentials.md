# ADR-0005: Kredensial Provider Dua Tingkat

- **Status:** Accepted
- **Tanggal:** 2026-10-12

## Konteks
Discovery inventory membutuhkan visibilitas lintas project (hypervisor, Placement, resource yang belum terpetakan), sedangkan least privilege menuntut mutasi dibatasi per Project. Application Credential OpenStack selalu ter-scope project dan melekat pada user pembuatnya; token Proxmox per pool tidak dapat melihat node/storage di luar pool.

## Keputusan
Entitas `ProviderCredential` dengan `purpose`:

| Purpose | OpenStack | Proxmox | Jumlah |
|---|---|---|---|
| `discovery` | Service user, role `reader` (system/domain scope) | Token `PVEAuditor` cluster-wide | 1 per provider |
| `mutate` | Application Credential per project | Token per pool + custom role | 1 per ProjectMapping |
| `capi` | Application Credential khusus CAPO | Token khusus CAPMOX | 1 per Project |
| `cluster` | Application Credential terbatas untuk CCM/CSI | Token terbatas untuk CCM/CSI | 1 per workload cluster |

## Konsekuensi
- Discovery lengkap tanpa memberi hak mutasi luas.
- Application Credential dibuat oleh **service user khusus**; rotasi = buat baru → tukar → hapus lama, dan harus merambat ke identity Secret CAPI.
- Kredensial `cluster` dapat dibaca cluster-admin workload cluster, sehingga cakupannya minimal dan dirotasi saat cluster dihapus.
- Daftar ACL Proxmox didokumentasikan di runbook dan diuji contract test mutating.

## Alternatif
- **Satu kredensial admin per provider:** sederhana, tetapi kebocoran = akses penuh.
- **Hanya kredensial per project:** tidak dapat melihat hypervisor, kapasitas, maupun resource `unassigned`.
