import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { json, renderRoutes, stubApi } from "@/shared/test/render";

import { ProvidersPage } from "./providers";

const admin = { username: "admin", roles: [{ role: "admin" }] };
const viewer = { username: "v", roles: [{ role: "viewer", projectId: "payment" }] };
const provider = {
  id: "pve-lab",
  type: "proxmox",
  name: "PVE Lab",
  siteId: "site-a",
  endpoints: ["https://pve1.example.com:8006"],
  regionName: "",
  fingerprint: "fp",
  createdAt: "",
  updatedAt: "",
};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function stub(me: unknown, onCreate?: (body: unknown) => void) {
  return stubApi({
    "GET /api/v1/auth/me": () => json(200, me),
    "GET /api/v1/providers": () => json(200, { items: [provider] }),
    "GET /api/v1/sites": () => json(200, { items: [{ id: "site-a", name: "Site A", labels: {}, createdAt: "", updatedAt: "" }] }),
    "POST /api/v1/providers": async (req) => {
      const body = await req.json();
      onCreate?.(body);
      return json(201, { ...provider, ...body });
    },
  });
}

describe("ProvidersPage", () => {
  it("lists providers with their site and links to the detail page", async () => {
    stub(viewer);
    renderRoutes([{ path: "/settings/providers", element: <ProvidersPage /> }], "/settings/providers");

    const link = await screen.findByRole("link", { name: "PVE Lab" });
    expect(link.getAttribute("href")).toBe("/settings/providers/pve-lab");
    expect(await screen.findByText("Site A")).toBeTruthy();
    expect(screen.queryByText("Register a provider")).toBeNull();
  });

  it("registers an OpenStack provider as admin", async () => {
    let sent: Record<string, unknown> | undefined;
    stub(admin, (b) => (sent = b as Record<string, unknown>));
    renderRoutes([{ path: "/settings/providers", element: <ProvidersPage /> }], "/settings/providers");

    await screen.findByText("Register a provider");
    fireEvent.change(screen.getByLabelText(/^ID/), { target: { value: "os-a" } });
    fireEvent.change(screen.getByLabelText(/^Name/), { target: { value: "OpenStack A" } });
    fireEvent.change(screen.getByLabelText(/^Endpoints/), { target: { value: " https://keystone.example.com:5000/v3 \n\n" } });
    fireEvent.change(screen.getByLabelText(/^Region/), { target: { value: "RegionOne" } });
    fireEvent.click(screen.getByRole("button", { name: "Register provider" }));

    expect(await screen.findByRole("status")).toBeTruthy();
    expect(sent).toEqual({
      id: "os-a",
      name: "OpenStack A",
      type: "openstack",
      endpoints: ["https://keystone.example.com:5000/v3"],
      regionName: "RegionOne",
    });
  });
});
