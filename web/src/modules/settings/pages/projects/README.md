# Projects page

- **Route:** `/settings/projects`
- **Permissions:** list for any signed-in user; project and mapping forms for global admins only
- **Data:** `GET|POST /api/v1/projects`, `GET|POST /api/v1/projects/{id}/mappings`, `GET /api/v1/providers`
- **Rules:** ownership is re-derived from mappings on the next sync, so a new mapping shows in Compute after up to one sync interval.
