# Module: compute

Owns virtual machines (and later images and templates).

- **Routes:** `/compute`, `/compute/vms/:providerId/:externalId`
- **Components:** `CapacityOverview` (exported; rendered on the system Overview page)
- **Uses:** `useGlobalFilter`, `useSiteOptions`, `useProjectOptions` from `@/modules/global-filter` (lists and capacity follow the global Site / Project filter)
- **Data:** `GET /api/v1/resources?kind=vm`, `GET /api/v1/resources/{providerId}/vm/{externalId}`, `GET /api/v1/capacity`, `GET /api/v1/providers` via `services/compute-service.ts`; query keys in `hooks/compute-keys.ts`
- **Rules:** inventory comes from SEMAR's cache; always show its age. Aggregates are scoped by the API exactly like lists; never compute totals client-side from a partial page.
