import createClient from "openapi-fetch";

import type { paths } from "./schema.gen";

/**
 * The single typed HTTP client. Paths, parameters and response bodies are
 * checked against api/openapi.yaml (regenerate with `npm run gen:api`).
 * Requests are same-origin; the Vite dev server proxies them to semar-api.
 */
export const apiClient = createClient<paths>({
  baseUrl: globalThis.location?.origin ?? "",
  // Resolve fetch per request (not once at import) so it can be replaced in tests.
  fetch: (request) => globalThis.fetch(request),
});
