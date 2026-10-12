# Overview page

- **Route:** `/` (index)
- **Permissions:** any signed-in user (auth arrives in Phase 1)
- **Data:** `useReadiness()` → `GET /readyz` (polled every 15 s), `useVersion()` → `GET /api/v1/version`
- **Next:** capacity overview and inventory summary once the inventory store exists (roadmap §3.5).
