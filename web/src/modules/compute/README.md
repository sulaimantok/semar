# Module: compute

Owns virtual machines (and later images and templates).

- **Routes:** `/compute`
- **Data:** `GET /api/v1/resources?kind=vm`, `GET /api/v1/providers` via `services/compute-service.ts`; query keys in `hooks/compute-keys.ts`
- **Rules:** inventory comes from SEMAR's cache; always show its age.
