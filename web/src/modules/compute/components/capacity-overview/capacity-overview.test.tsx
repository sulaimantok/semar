import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { json, renderRoutes, stubApi } from "@/shared/test/render";

import { CapacityOverview } from "./capacity-overview";

const vms = {
  total: 3,
  vcpus: 12,
  memoryMiB: 24576,
  byStatus: [
    { key: "running", count: 2 },
    { key: "stopped", count: 1 },
  ],
  byArch: [{ key: "arm64", count: 3 }],
};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("CapacityOverview", () => {
  it("shows VM totals and physical capacity for global roles", async () => {
    stubApi({
      "GET /api/v1/capacity": () =>
        json(200, {
          vms,
          hypervisors: { total: 2, vcpus: 64, vcpusUsed: 16, memoryMiB: 262144, memoryMiBUsed: 65536, byArch: [{ key: "arm64", count: 2 }] },
        }),
    });
    renderRoutes([{ path: "/", element: <CapacityOverview /> }], "/");

    expect(await screen.findByText("running · 2")).toBeTruthy();
    expect(screen.getByText("24 GiB memory")).toBeTruthy();
    expect(screen.getByText("16 / 64 (25%)")).toBeTruthy();
    expect(screen.getByLabelText("Memory used").getAttribute("value")).toBe("25");
  });

  it("omits physical capacity for scoped roles", async () => {
    stubApi({ "GET /api/v1/capacity": () => json(200, { vms }) });
    renderRoutes([{ path: "/", element: <CapacityOverview /> }], "/");

    expect(await screen.findByText("arm64 · 3")).toBeTruthy();
    expect(screen.queryByText("Hypervisors")).toBeNull();
  });

  it("explains an empty inventory", async () => {
    stubApi({
      "GET /api/v1/capacity": () => json(200, { vms: { total: 0, vcpus: 0, memoryMiB: 0, byStatus: [], byArch: [] } }),
    });
    renderRoutes([{ path: "/", element: <CapacityOverview /> }], "/");

    expect(await screen.findByText(/No inventory yet/)).toBeTruthy();
  });
});
