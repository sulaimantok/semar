# SEMAR

**System for Enterprise Multi-cloud & Application Runtime** — satu control plane untuk VM di OpenStack & Proxmox VE, cluster Kubernetes (RKE2 via Cluster API), dan observability terpadu, dengan x86_64 dan ARM64 sebagai warga kelas satu.

> **Status:** Phase 0 (fondasi). Belum siap dipakai.

## Isi Saat Ini

| Komponen | Lokasi | Keterangan |
|---|---|---|
| API server | `cmd/semar-api`, `internal/api` | `/healthz`, `/readyz`, `/api/v1/version`; error RFC 9457; `X-Request-ID` |
| Kontrak OpenAPI | `api/openapi.yaml` | Sumber kontrak frontend ↔ backend |
| Kontrak driver | `internal/driver/contract` | Model provider-neutral + interface `CloudDriver` (read-only) |
| Contract test suite | `internal/driver/contracttest` | Dijalankan setiap driver (Fake per PR, lab nightly) |
| Fake Driver | `internal/driver/fake` | Inventory deterministik + fault injection (latency, error rate, fail-next) |
| ADR | `docs/adr/` | ADR-0001 … ADR-0007 |

## Pengembangan

Prasyarat: Go 1.26+, `golangci-lint` v2, Docker Buildx (opsional).

```bash
make test      # go test -race
make lint      # golangci-lint
make build     # bin/semar-api
make run-api   # listen di :8080
make image     # image multi-arch linux/amd64 + linux/arm64
```

Konfigurasi `semar-api` melalui environment:

| Variabel | Default | Keterangan |
|---|---|---|
| `SEMAR_HTTP_ADDR` | `:8080` | Alamat listen |
| `SEMAR_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `SEMAR_SHUTDOWN_TIMEOUT` | `15s` | Batas graceful shutdown |

## Menambah Driver Provider

1. Implementasikan `contract.CloudDriver` (dan interface opsional seperti `contract.IncrementalVMLister` bila didukung).
2. Bungkus error provider dengan sentinel di `contract` (`ErrUnavailable`, `ErrUnauthorized`, …).
3. Jalankan suite yang sama di test driver:

```go
func TestReadOnlyContract(t *testing.T) {
	contracttest.RunReadOnly(t, func(t *testing.T) contract.CloudDriver {
		return mydriver.New(...)
	})
}
```

## Dokumen Perencanaan

Roadmap, implementation tracker, dan technical notes dikelola di workspace perencanaan (`tech-garden-dev/opensources/recommendation/semar/`). Keputusan arsitektur yang mengikat ada di [`docs/adr/`](docs/adr/).
