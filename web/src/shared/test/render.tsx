import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactNode } from "react";
import { createMemoryRouter, RouterProvider, type RouteObject } from "react-router";
import { vi } from "vitest";

export const json = (status: number, body: unknown, type = "application/json") =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": type } });

export const problem = (status: number, title: string, detail?: string) =>
  json(status, { type: "about:blank", title, status, detail }, "application/problem+json");

/** Routes a stubbed fetch by "METHOD /path"; unmatched requests answer 404. */
export function stubApi(routes: Record<string, (req: Request) => Response | Promise<Response>>) {
  const calls: Request[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      calls.push(req);
      const handler = routes[`${req.method} ${new URL(req.url).pathname}`];
      return handler ? handler(req) : problem(404, "Not Found");
    }),
  );
  return calls;
}

/** Renders routes in a memory router with a fresh query client. */
export function renderRoutes(routes: RouteObject[], initialPath: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const router = createMemoryRouter(routes, { initialEntries: [initialPath] });
  const wrap = (node: ReactNode) => <QueryClientProvider client={client}>{node}</QueryClientProvider>;
  render(wrap(<RouterProvider router={router} />));
  return router;
}
