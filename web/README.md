# SEMAR Web

React + Vite + TypeScript + Tailwind single-page app for SEMAR.

## Pages

| Route | Module | What it shows |
|---|---|---|
| `/login` | `auth` | Sign-in (uniform error messages, rate-limit notice, same-app redirects only) |
| `/` | `system` | API status, build, and the Capacity Overview from `compute` |
| `/compute` | `compute` | VM list from the inventory cache with data age, provider filter and cursor paging |
| `/settings/providers`, `/settings/providers/:id` | `settings` | Providers, their sync status, write-only credentials, removal |
| `/settings/sites`, `/settings/projects` | `settings` | Sites; projects and their mappings to OpenStack projects / Proxmox pools |

Every page is scoped by the API to the signed-in user's role bindings. Forms that change settings render for global admins only; the API enforces the same rule.

## Layout

Follows a module-based architecture:

- `src/routes/` — thin route table; mounts module pages inside the app shell.
- `src/modules/<feature>/` — one folder per product capability. Other code imports a module **only** through its `index.ts` barrel (enforced by ESLint).
- `src/modules/<feature>/pages/<page>/` — every route is a folder with its component, `*.styles.ts`, `index.ts` and `README.md`.
- `src/shared/api-client/` — the only code that talks to the network. Types are generated from `../api/openapi.yaml`.
- Server state lives in TanStack Query hooks; UI state never mirrors server data. Styles live in co-located `*.styles.ts` files, never inline.

## Commands

```bash
npm ci
npm run dev        # http://localhost:5173, proxies /api, /healthz, /readyz to SEMAR_API_URL (default http://127.0.0.1:8080)
npm run gen:api    # regenerate src/shared/api-client/schema.gen.ts after editing api/openapi.yaml
npm run typecheck
npm run lint
npm test
npm run build
```

Navigation entries for features that are not released yet stay hidden (`released: false` in `src/shared/components/app-shell/nav-items.ts`).
