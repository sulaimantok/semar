# Providers page

- **Route:** `/settings/providers`
- **Permissions:** list for any signed-in user; the registration form is shown to global admins only
- **Data:** `GET /api/v1/providers`, `GET /api/v1/sites`, `POST /api/v1/providers`
- **Rules:** the server derives the provider fingerprint; the form never sends one.
