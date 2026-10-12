# ADR-0002: Workflow Engine — Temporal

- **Status:** Accepted
- **Tanggal:** 2026-10-12

## Konteks
Provisioning VM dan operasi cluster bersifat panjang dan rawan gagal sebagian (timeout jaringan, API provider lambat, worker restart). Sync inventory per provider harus singleton dan punya backoff.

## Keputusan
Gunakan **Temporal** sejak Phase 1:
- **Schedules** per provider untuk sync inventory (overlap policy SKIP).
- **Workflow** untuk seluruh mutasi VM dan cluster, mengikuti konvensi idempotency (roadmap §3.8).
- **Task queue per site** (`site-<id>`) agar worker dapat dipindah ke site remote.

## Konsekuensi
- Retry, timeout, kompensasi (Saga), dan ketahanan restart tersedia tanpa membangun state machine sendiri.
- Activity **wajib idempotent** (find-or-create bertag `semar.op_id`) karena retry dapat terjadi setelah panggilan provider berhasil.
- Payload & persistensi Temporal adalah permukaan sensitif: tidak boleh ada secret di payload, PayloadCodec mengenkripsi payload, mTLS ke frontend, UI hanya untuk Admin.
- Kode workflow wajib di-versioning dan diuji dengan replay test di CI.
- Komponen tambahan untuk dioperasikan; pada profil Lite memakai PostgreSQL yang sama (database terpisah) dan SQL visibility.

## Alternatif
- **Asynq/Celery + state machine di PostgreSQL:** lebih ringan, tetapi retry/kompensasi/singleton harus dibangun sendiri dan rawan bug.
- **Goroutine + ticker untuk sync:** cukup untuk read-only, tetapi tidak memberi singleton lintas replika maupun task queue per site.
