# ADR-0007: Objek CAPI sebagai Sumber Kebenaran State Cluster

- **Status:** Accepted
- **Tanggal:** 2026-10-12

## Konteks
State cluster dapat berubah di luar SEMAR (reconciliation CAPI, `kubectl` ke management cluster, remediation otomatis). Menyimpan state cluster sebagai state workflow Temporal akan menyimpang dari kenyataan.

## Keputusan
- **Objek CAPI di management cluster adalah sumber kebenaran.** Database SEMAR hanya cache.
- Workflow Temporal hanya **mengirim perubahan topology** lalu menunggu conditions v1beta2 (`Available`, `RollingOut`, `ScalingUp/Down`, `Remediating`) dengan timeout.
- State di UI **diturunkan dari conditions**, bukan dari state workflow.
- VM milik cluster CAPI bersifat read-only di VM Manager.

## Konsekuensi
- Tidak ada konflik antara workflow dan reconciliation.
- SEMAR membutuhkan watch/poll terhadap objek CAPI dan pemetaan conditions → state UI.
- Kehilangan database SEMAR tidak menghilangkan state cluster; cache dapat dibangun ulang dari management cluster.

## Alternatif
- **State machine cluster di workflow/database SEMAR:** rawan divergensi dan menduplikasi logika CAPI.
