# VM list page

- **Route:** `/compute`
- **Permissions:** signed in; the API returns only VMs the caller's role bindings allow (unassigned VMs for global admin/operator only)
- **Data:** `useVmPages(providerId)` → `GET /api/v1/resources?kind=vm` (cursor pagination, background refresh every 60 s); `useProviders()` → `GET /api/v1/providers` for names and the filter
- **Shows:** data age per row ("Updated"), not a real-time claim (roadmap §3.6)
