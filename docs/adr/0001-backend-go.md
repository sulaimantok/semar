# ADR-0001: Bahasa Backend — Go

- **Status:** Accepted
- **Tanggal:** 2026-10-12

## Konteks
SEMAR berinteraksi dengan OpenStack, Proxmox VE, Kubernetes/Cluster API, dan Temporal, serta harus berjalan di `linux/amd64` dan `linux/arm64`. Tim berjumlah 1–2 engineer; sebagian terbiasa dengan Python (FastAPI).

## Keputusan
Seluruh backend inti (`semar-api`, `semar-worker`) ditulis dalam **Go**. Tidak ada bahasa kedua di backend inti.

## Konsekuensi
- Integrasi native: `gophercloud`, `client-go`/`controller-runtime` (CAPI v1beta2), Temporal Go SDK.
- Single static binary per arsitektur; image distroless kecil; cross-compile tanpa emulasi.
- Kurva belajar bagi anggota tim yang terbiasa Python; perlu konvensi kode dan linting sejak awal (`golangci-lint`).

## Alternatif
- **Python (FastAPI):** cepat untuk MVP dan familiar, tetapi integrasi CAPI/controller-runtime dan Temporal kurang native, serta distribusi multi-arch lebih berat.
