import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { json, renderRoutes, stubApi } from "@/shared/test/render";

import { VmListPage } from "./vm-list";

const vm = (id: string, extra: Record<string, unknown> = {}) => ({
  providerId: "pve-lab",
  kind: "vm",
  externalId: id,
  name: `vm-${id}`,
  externalScope: "",
  arch: "arm64",
  status: "running",
  spec: { vcpus: 4, memoryMiB: 8192, ipAddresses: ["10.0.0.5"] },
  observedAt: new Date(Date.now() - 30_000).toISOString(),
  firstSeenAt: new Date().toISOString(),
  ...extra,
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("VmListPage", () => {
  it("renders VMs with provider names, data age and unassigned badge", async () => {
    stubApi({
      "GET /api/v1/providers": () =>
        json(200, { items: [{ id: "pve-lab", name: "PVE Lab", type: "proxmox", endpoints: [], regionName: "", fingerprint: "", createdAt: "", updatedAt: "" }] }),
      "GET /api/v1/resources": () => json(200, { items: [vm("100", { projectId: "payment" }), vm("101")] }),
    });
    renderRoutes([{ path: "/compute", element: <VmListPage /> }], "/compute");

    const row = (await screen.findByText("vm-100")).closest("tr")!;
    const cells = within(row);
    expect(await cells.findByText("PVE Lab")).toBeTruthy();
    expect(cells.getByText("payment")).toBeTruthy();
    expect(cells.getByText("8 GiB")).toBeTruthy();
    expect(cells.getByText("10.0.0.5")).toBeTruthy();
    expect(cells.getByText(/s ago$/)).toBeTruthy();
    expect(within(screen.getByText("vm-101").closest("tr")!).getByText("unassigned")).toBeTruthy();
  });

  it("loads further pages with the cursor", async () => {
    const calls = stubApi({
      "GET /api/v1/providers": () => json(200, { items: [] }),
      "GET /api/v1/resources": (req) =>
        new URL(req.url).searchParams.get("cursor") === "c1"
          ? json(200, { items: [vm("2")] })
          : json(200, { items: [vm("1")], nextCursor: "c1" }),
    });
    renderRoutes([{ path: "/compute", element: <VmListPage /> }], "/compute");
    fireEvent.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByText("vm-2")).toBeTruthy();
    expect(screen.getByText("2 shown")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
    const vmCalls = calls.filter((c) => new URL(c.url).pathname === "/api/v1/resources");
    expect(new URL(vmCalls[0]!.url).searchParams.get("kind")).toBe("vm");
  });

  it("shows an empty state", async () => {
    stubApi({
      "GET /api/v1/providers": () => json(200, { items: [] }),
      "GET /api/v1/resources": () => json(200, { items: [] }),
    });
    renderRoutes([{ path: "/compute", element: <VmListPage /> }], "/compute");
    expect(await screen.findByText(/No virtual machines yet/)).toBeTruthy();
  });
});
