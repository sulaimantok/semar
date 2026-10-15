# Module: system

Owns the control plane's own status: API readiness and build information.

- **Routes:** `/` (Overview), `*` (Not found)
- **Data:** `GET /readyz`, `GET /api/v1/version` via `services/system-service.ts`; query keys in `hooks/system-keys.ts`
- **Uses:** `CapacityOverview` from `@/modules/compute` on the Overview page
- **Rules:** a 503 from `/readyz` is a valid "not ready" answer, not an error.
