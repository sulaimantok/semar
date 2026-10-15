# Provider detail page

- **Route:** `/settings/providers/:providerId`
- **Permissions:** facts, sync status and the last connection check for any signed-in user in scope (the API hides `lastError` from non-admins); editing, connection checks, credentials and removal for global admins only
- **Data:** `GET /api/v1/providers/{id}`, `GET …/sync-status` (polled every 30 s), `GET|PUT|DELETE …/credentials`, `PATCH /api/v1/providers/{id}`, `POST …/validate`, `DELETE /api/v1/providers/{id}`
- **Rules:** secrets are write-only — inputs are password fields, cleared after every submit, and never read back. Removal needs a second confirming click. The edit form sends only changed fields and warns that a new address clears the connection check and that a site change moves the sync to that site's worker.
