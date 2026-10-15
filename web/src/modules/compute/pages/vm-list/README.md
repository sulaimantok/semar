# VM list page

- **Route:** `/compute`
- **Permissions:** signed in; the API returns only VMs the caller's role bindings allow (unassigned VMs for global admin/operator only)
- **Data:** `useVmPages(filter)` → `GET /api/v1/resources?kind=vm` with the global Site / Project filter plus the page's Provider and Arch filters (cursor pagination, background refresh every 60 s); `useProviders()`, `useSiteOptions()`, `useProjectOptions()` for names
- **Filters:** every active filter is shown as a removable chip; removing a global one also clears it in the filter bar
- **Shows:** data age per row ("Updated"), not a real-time claim (roadmap §3.6)
