# ADR-0006: Tenancy Observability

- **Status:** Accepted
- **Tanggal:** 2026-10-12

## Konteks
RBAC SEMAR di-scope per Project, tetapi exporter tidak mengenal Project SEMAR dan Grafana embed mengakses datasource secara langsung. Filter label berbasis manipulasi string query mudah di-bypass.

## Keputusan
1. Label `semar_project`, `semar_provider`, `semar_site` ditempelkan **saat ingest** (vmagent relabel / file_sd yang dihasilkan SEMAR dari pemetaan Project).
2. Semua query (widget native dan Grafana) melewati **proxy penegak label** (`vmauth` / `prom-label-proxy`) berdasarkan identitas user.
3. Log memakai **tenant Loki** (`X-Scope-OrgID`) per Project.
4. Grafana hanya memakai datasource yang diproksi; sebelum proxy siap, Grafana embed hanya untuk Admin/Operator.
5. Workload cluster menulis metrik dengan token per cluster; label `cluster` ditegakkan di server.

## Konsekuensi
- Pemetaan Project harus tersedia sebelum metrik dibuka ke non-Admin (Phase 2.5).
- Exit criteria menyertakan uji bypass melalui query bebas dan Grafana.

## Alternatif
- **Filter di backend dengan menambah label ke string query:** mudah di-bypass.
- **Instance observability per Project:** isolasi kuat, tetapi beban operasional terlalu besar.
