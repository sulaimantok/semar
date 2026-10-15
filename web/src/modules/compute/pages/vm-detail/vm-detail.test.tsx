import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { json, problem, renderRoutes, stubApi } from "@/shared/test/render";

import { VmDetailPage } from "./vm-detail";

const resource = {
  providerId: "pve-lab",
  kind: "vm",
  externalId: "100",
  name: "web-1",
  externalScope: "pool:payment",
  projectId: "payment",
  arch: "arm64",
  status: "running",
  spec: { vcpus: 4, memoryMiB: 8192, ipAddresses: ["192.0.2.10"], hypervisorId: "pve1", tags: ["prod"] },
  observedAt: new Date(Date.now() - 30_000).toISOString(),
  firstSeenAt: "2026-10-01T00:00:00Z",
};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function render(detail: () => Response) {
  stubApi({
    "GET /api/v1/resources/pve-lab/vm/100": detail,
    "GET /api/v1/providers": () =>
      json(200, {
        items: [{ id: "pve-lab", type: "proxmox", name: "PVE Lab", siteId: "site-a", endpoints: [], regionName: "", fingerprint: "", createdAt: "", updatedAt: "" }],
      }),
    "GET /api/v1/sites": () => json(200, { items: [{ id: "site-a", name: "Site A", labels: {}, createdAt: "", updatedAt: "" }] }),
    "GET /api/v1/projects": () => json(200, { items: [{ id: "payment", name: "Payment", createdAt: "", updatedAt: "" }] }),
  });
  renderRoutes([{ path: "/compute/vms/:providerId/:externalId", element: <VmDetailPage /> }], "/compute/vms/pve-lab/100");
}

describe("VmDetailPage", () => {
  it("shows location, spec and history", async () => {
    render(() =>
      json(200, {
        resource,
        changes: [
          { id: 2, change: "updated", observedAt: "2026-10-02T00:00:00Z", projectId: "payment" },
          { id: 1, change: "created", observedAt: "2026-10-01T00:00:00Z" },
        ],
      }),
    );
    expect(await screen.findByRole("heading", { name: "web-1" })).toBeTruthy();
    expect(await screen.findByText("Site A")).toBeTruthy();
    expect(await screen.findByText("Payment")).toBeTruthy();
    expect(screen.getByText("8 GiB")).toBeTruthy();
    expect(screen.getByText("192.0.2.10")).toBeTruthy();
    expect(screen.getByText("updated")).toBeTruthy();
    expect(screen.getByText("created")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("flags an orphaned VM", async () => {
    render(() => json(200, { resource: { ...resource, orphanedAt: "2026-10-03T00:00:00Z" }, changes: [] }));
    expect((await screen.findByRole("alert")).textContent).toMatch(/Not reported by the provider/);
  });

  it("treats a hidden VM like a missing one", async () => {
    render(() => problem(404, "Not Found", "not found"));
    expect(await screen.findByText(/does not exist or is outside the projects you can see/)).toBeTruthy();
  });
});
