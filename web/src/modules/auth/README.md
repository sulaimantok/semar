# Module: auth

Owns sign-in, sign-out and the current user.

- **Routes:** `/login`
- **Data:** `GET /api/v1/auth/me`, `POST /api/v1/auth/login`, `POST /api/v1/auth/logout` via `services/auth-service.ts`; query keys in `hooks/auth-keys.ts`
- **Rules:** `RequireAuth` guards every route inside the app shell. A 401 from any query clears `authKeys.me()` (see `main.tsx`), which sends the user back to `/login`. Logout clears the whole query cache. `useIsAdmin()` / `isGlobalAdmin()` only hide controls the API would refuse; the API stays the authority.
