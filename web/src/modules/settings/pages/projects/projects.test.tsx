import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { json, renderRoutes, stubApi } from "@/shared/test/render";

import { ProjectsPage } from "./projects";

const admin = { username: "admin", roles: [{ role: "admin" }] };

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("ProjectsPage", () => {
  it("lists mappings per project and adds one as admin", async () => {
    const mappings = [{ id: 1, projectId: "payment", providerId: "pve-lab", externalScope: "pool:payment", createdAt: "" }];
    let sent: unknown;
    stubApi({
      "GET /api/v1/auth/me": () => json(200, admin),
      "GET /api/v1/projects": () => json(200, { items: [{ id: "payment", name: "Payment", createdAt: "", updatedAt: "" }] }),
      "GET /api/v1/providers": () =>
        json(200, {
          items: [{ id: "pve-lab", type: "proxmox", name: "PVE Lab", endpoints: [], regionName: "", fingerprint: "", createdAt: "", updatedAt: "" }],
        }),
      "GET /api/v1/projects/payment/mappings": () => json(200, { items: mappings }),
      "POST /api/v1/projects/payment/mappings": async (req) => {
        sent = await req.json();
        mappings.push({ id: 2, projectId: "payment", providerId: "pve-lab", externalScope: "pool:billing", createdAt: "" });
        return json(201, mappings[1]);
      },
    });
    renderRoutes([{ path: "/settings/projects", element: <ProjectsPage /> }], "/settings/projects");

    const list = await screen.findByRole("list", { name: "Mappings of Payment" });
    expect(await within(list).findByText("pool:payment")).toBeTruthy();
    expect(within(list).getByText("PVE Lab")).toBeTruthy();

    const form = screen.getByRole("form", { name: "Add mapping to payment" });
    expect(within(form).getByText(/^pool:<name>, or tag:<tag>/)).toBeTruthy();
    fireEvent.change(within(form).getByLabelText(/External scope/), { target: { value: "pool:billing" } });
    fireEvent.click(within(form).getByRole("button", { name: "Add mapping" }));

    expect(await within(list).findByText("pool:billing")).toBeTruthy();
    expect(sent).toEqual({ providerId: "pve-lab", externalScope: "pool:billing" });
  });
});
