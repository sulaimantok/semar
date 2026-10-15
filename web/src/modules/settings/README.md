# Module: settings

Owns where resources live (sites, providers), who owns them (projects and their mappings), and provider credentials.

- **Routes:** `/settings` → `/settings/providers`, `/settings/providers/:providerId`, `/settings/sites`, `/settings/projects`
- **Data:** `/api/v1/sites`, `/api/v1/projects`, `/api/v1/projects/{id}/mappings`, `/api/v1/providers`, `/api/v1/providers/{id}`, `…/sync-status`, `…/credentials` via `services/settings-service.ts`; query keys in `hooks/settings-keys.ts`
- **Uses:** `useIsAdmin` from `@/modules/auth`; `computeKeys` from `@/modules/compute` (provider and mapping changes refresh inventory views)
- **Rules:** every signed-in user can read settings; forms render for global admins only, and the API enforces the same rule. Secrets are write-only and never stored in query state.
