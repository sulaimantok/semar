import { describe, expect, it } from "vitest";

import { NAV_ITEMS, visibleNavItems } from "./nav-items";

describe("visibleNavItems", () => {
  it("hides unreleased features", () => {
    const labels = visibleNavItems().map((item) => item.label);
    expect(labels).toContain("Overview");
    expect(labels).not.toContain("Compute");
  });

  it("keeps the resource-type order", () => {
    const released = NAV_ITEMS.map((item) => ({ ...item, released: true }));
    expect(visibleNavItems(released).map((item) => item.label)).toEqual([
      "Overview",
      "Compute",
      "Kubernetes",
      "Network",
      "Storage",
      "Observability",
      "Settings",
    ]);
  });

  it("uses unique routes", () => {
    const routes = NAV_ITEMS.map((item) => item.to);
    expect(new Set(routes).size).toBe(routes.length);
  });
});
