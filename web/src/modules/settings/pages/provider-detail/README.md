# Provider detail page

- **Route:** `/settings/providers/:providerId`
- **Permissions:** facts and sync status for any signed-in user in scope (the API hides `lastError` from non-admins); credentials and removal for global admins only
- **Data:** `GET /api/v1/providers/{id}`, `GET …/sync-status` (polled every 30 s), `GET|PUT|DELETE …/credentials`, `DELETE /api/v1/providers/{id}`
- **Rules:** secrets are write-only — inputs are password fields, cleared after every submit, and never read back. Removal needs a second confirming click.
