# Projects page

- **Route:** `/settings/projects`
- **Permissions:** list for any signed-in user; creating, editing and removing (two-step) projects and mappings for global admins only
- **Data:** `GET|POST /api/v1/projects`, `GET|POST /api/v1/projects/{id}/mappings`, `PATCH|DELETE /api/v1/projects/{id}/mappings/{mappingId}`, `GET /api/v1/providers`
- **Rules:** ownership is re-derived from mappings on the next sync, so a new mapping shows in Compute after up to one sync interval.
