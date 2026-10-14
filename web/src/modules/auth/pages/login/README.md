# Login page

- **Route:** `/login` (outside the app shell); `?next=/path` returns there after sign-in (same-app paths only)
- **Permissions:** public; signed-in users are redirected to `next`
- **Data:** `useLogin()` → `POST /api/v1/auth/login`; `useMe()` → `GET /api/v1/auth/me`
- **Errors:** 401 → invalid credentials, 429 → rate limited (same message for unknown users)
