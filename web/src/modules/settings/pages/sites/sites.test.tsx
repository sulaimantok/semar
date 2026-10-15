import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { json, renderRoutes, stubApi } from "@/shared/test/render";

import { SitesPage } from "./sites";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("SitesPage", () => {
  it("creates a site with parsed labels and blocks malformed ones", async () => {
    let sent: unknown;
    stubApi({
      "GET /api/v1/auth/me": () => json(200, { username: "admin", roles: [{ role: "admin" }] }),
      "GET /api/v1/sites": () => json(200, { items: [] }),
      "POST /api/v1/sites": async (req) => {
        sent = await req.json();
        return json(201, { ...(sent as object), labels: {}, createdAt: "", updatedAt: "" });
      },
    });
    renderRoutes([{ path: "/settings/sites", element: <SitesPage /> }], "/settings/sites");

    await screen.findByText("Add a site");
    fireEvent.change(screen.getByLabelText(/^ID/), { target: { value: "site-a" } });
    fireEvent.change(screen.getByLabelText(/^Name/), { target: { value: "Site A" } });
    const labels = screen.getByLabelText(/^Labels/);
    const submit = screen.getByRole("button", { name: "Add site" }) as HTMLButtonElement;

    fireEvent.change(labels, { target: { value: "env" } });
    expect(submit.disabled).toBe(true);
    fireEvent.change(labels, { target: { value: "env=prod, tier=1" } });
    fireEvent.click(submit);

    await vi.waitFor(() => expect(sent).toEqual({ id: "site-a", name: "Site A", labels: { env: "prod", tier: "1" } }));
  });
});
