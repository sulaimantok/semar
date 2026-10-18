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

  it("edits and removes a mapping as admin", async () => {
    let mappings = [{ id: 1, projectId: "payment", providerId: "pve-lab", externalScope: "pool:payment", createdAt: "" }];
    let patched: unknown;
    const deleted = vi.fn();
    stubApi({
      "GET /api/v1/auth/me": () => json(200, admin),
      "GET /api/v1/projects": () => json(200, { items: [{ id: "payment", name: "Payment", createdAt: "", updatedAt: "" }] }),
      "GET /api/v1/providers": () =>
        json(200, {
          items: [{ id: "pve-lab", type: "proxmox", name: "PVE Lab", endpoints: [], regionName: "", fingerprint: "", tlsFingerprint: "", createdAt: "", updatedAt: "" }],
        }),
      "GET /api/v1/projects/payment/mappings": () => json(200, { items: mappings }),
      "PATCH /api/v1/projects/payment/mappings/1": async (req) => {
        patched = await req.json();
        mappings = [{ ...mappings[0]!, externalScope: "tag:payment" }];
        return json(200, mappings[0]);
      },
      "DELETE /api/v1/projects/payment/mappings/1": () => {
        deleted();
        mappings = [];
        return new Response(null, { status: 204 });
      },
    });
    renderRoutes([{ path: "/settings/projects", element: <ProjectsPage /> }], "/settings/projects");

    fireEvent.click(await screen.findByRole("button", { name: "Edit mapping PVE Lab → pool:payment" }));
    const form = screen.getByRole("form", { name: "Edit mapping PVE Lab → pool:payment" });
    fireEvent.change(within(form).getByLabelText(/External scope/), { target: { value: "tag:payment" } });
    fireEvent.click(within(form).getByRole("button", { name: "Save" }));
    const list = await screen.findByRole("list", { name: "Mappings of Payment" });
    expect(await within(list).findByText("tag:payment")).toBeTruthy();
    expect(patched).toEqual({ externalScope: "tag:payment" });

    fireEvent.click(screen.getByRole("button", { name: "Remove mapping PVE Lab → tag:payment" }));
    expect(deleted).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Confirm remove" }));
    expect(await screen.findByText(/No mappings/)).toBeTruthy();
    expect(deleted).toHaveBeenCalledOnce();
  });

  it("does not offer editing to non-admins", async () => {
    stubApi({
      "GET /api/v1/auth/me": () => json(200, { username: "v", roles: [{ role: "viewer" }] }),
      "GET /api/v1/projects": () => json(200, { items: [{ id: "payment", name: "Payment", createdAt: "", updatedAt: "" }] }),
      "GET /api/v1/providers": () => json(200, { items: [] }),
      "GET /api/v1/projects/payment/mappings": () =>
        json(200, { items: [{ id: 1, projectId: "payment", providerId: "pve-lab", externalScope: "pool:payment", createdAt: "" }] }),
    });
    renderRoutes([{ path: "/settings/projects", element: <ProjectsPage /> }], "/settings/projects");
    expect(await screen.findByText("pool:payment")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Edit mapping/ })).toBeNull();
  });
});
