# Overview page

- **Route:** `/` (index)
- **Permissions:** any signed-in user; capacity is scoped to the user's role bindings by the API
- **Data:** `useReadiness()` → `GET /readyz` (polled every 15 s), `useVersion()` → `GET /api/v1/version`, `CapacityOverview` → `GET /api/v1/capacity` (polled every 60 s)
- **Next:** grouping capacity per Site / Provider / Project (Phase 1.x).
