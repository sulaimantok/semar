# Module: global-filter

Owns the global Site / Project filter shown above every page (roadmap §3.5).

- **Components:** `GlobalFilterBar` (mounted in the app shell header by the router)
- **Hooks:** `useGlobalFilter()` → `{ siteId, projectId, isActive, set, reset }`, consumed by inventory views (compute)
- **Data:** `GET /api/v1/sites`, `GET /api/v1/projects` for the options; query keys in `hooks/global-filter-keys.ts` (the settings module invalidates them after changes)
- **Store:** `globalFilterStore` (`get`, `set`, `reset`, `subscribe`), persisted per viewer in localStorage (guarded; falls back to All)
- **Rules:** the filter only narrows what the API already allows; it is never an access control. Defaults to All, the active state is always visible, and stale choices are dropped.
