import { cleanup, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { json, problem, renderRoutes, stubApi } from "@/shared/test/render";

import { RequireAuth } from "./require-auth";

const routes = [
  { path: "/login", element: <p>login page</p> },
  { path: "/compute", element: <RequireAuth><p>secret page</p></RequireAuth> },
];

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("RequireAuth", () => {
  it("redirects anonymous users to /login with next", async () => {
    stubApi({ "GET /api/v1/auth/me": () => problem(401, "Unauthorized") });
    const router = renderRoutes(routes, "/compute");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toBe("?next=%2Fcompute");
  });

  it("renders the page for signed-in users", async () => {
    stubApi({ "GET /api/v1/auth/me": () => json(200, { username: "admin", roles: [] }) });
    renderRoutes(routes, "/compute");
    expect(await screen.findByText("secret page")).toBeTruthy();
  });
});
