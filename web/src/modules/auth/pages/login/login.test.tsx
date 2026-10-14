import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { json, problem, renderRoutes, stubApi } from "@/shared/test/render";

import { LoginPage } from "./login";

const routes = [
  { path: "/login", element: <LoginPage /> },
  { path: "/compute", element: <p>compute page</p> },
  { path: "/", element: <p>home page</p> },
];

function fillAndSubmit(username: string, password: string) {
  fireEvent.change(screen.getByLabelText("Username"), { target: { value: username } });
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: password } });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("LoginPage", () => {
  it("shows a uniform message for bad credentials", async () => {
    stubApi({
      "GET /api/v1/auth/me": () => problem(401, "Unauthorized"),
      "POST /api/v1/auth/login": () => problem(401, "Unauthorized", "invalid username or password"),
    });
    renderRoutes(routes, "/login");
    fillAndSubmit("admin", "wrong");
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "Invalid username or password.");
  });

  it("explains rate limiting", async () => {
    stubApi({
      "GET /api/v1/auth/me": () => problem(401, "Unauthorized"),
      "POST /api/v1/auth/login": () => problem(429, "Too Many Requests"),
    });
    renderRoutes(routes, "/login");
    fillAndSubmit("admin", "x");
    expect((await screen.findByRole("alert")).textContent).toContain("Too many attempts");
  });

  it("signs in, sends the CSRF header and returns to next", async () => {
    const calls = stubApi({
      "GET /api/v1/auth/me": () => problem(401, "Unauthorized"),
      "POST /api/v1/auth/login": () => json(200, { username: "admin", roles: [{ role: "admin" }] }),
    });
    const router = renderRoutes(routes, "/login?next=%2Fcompute");
    fillAndSubmit("admin", "correct-password");
    await waitFor(() => expect(router.state.location.pathname).toBe("/compute"));
    const loginCall = calls.find((c) => c.method === "POST");
    expect(loginCall?.headers.get("X-Requested-By")).toBe("semar-web");
  });

  it("refuses off-site redirect targets", async () => {
    stubApi({
      "GET /api/v1/auth/me": () => problem(401, "Unauthorized"),
      "POST /api/v1/auth/login": () => json(200, { username: "admin", roles: [] }),
    });
    const router = renderRoutes(routes, "/login?next=%2F%2Fevil.example");
    fillAndSubmit("admin", "correct-password");
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
  });
});
