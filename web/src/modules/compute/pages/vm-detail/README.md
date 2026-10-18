# VM detail page

- **Route:** `/compute/vms/:providerId/:externalId`
- **Permissions:** signed in; the API answers 404 for VMs outside the caller's scope, shown the same as a missing VM
- **Data:** `useVm()` → `GET /api/v1/resources/{providerId}/vm/{externalId}` (resource + latest 50 changes, refreshed every 60 s); provider, site and project names from `useProviders()` and the global-filter options
- **Shows:** location and ownership, specification, data age, history; orphaned VMs carry a banner and stay reachable for their history
- **Actions:** VNC and serial console links for Admin/Operator bindings covering the VM (`canOperate`), hidden for orphaned VMs
