# SEMAR Web

React + Vite + TypeScript + Tailwind single-page app for SEMAR.

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
