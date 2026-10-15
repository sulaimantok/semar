import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { json, renderRoutes, stubApi } from "@/shared/test/render";

import { globalFilterStore } from "../../stores/global-filter-store";

import { GlobalFilterBar } from "./global-filter-bar";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
  globalFilterStore.reload();
});

function stub() {
  stubApi({
    "GET /api/v1/sites": () => json(200, { items: [{ id: "site-a", name: "Site A", labels: {}, createdAt: "", updatedAt: "" }] }),
    "GET /api/v1/projects": () => json(200, { items: [{ id: "payment", name: "Payment", createdAt: "", updatedAt: "" }] }),
  });
}

describe("GlobalFilterBar", () => {
  it("defaults to All, shows an active indicator and resets", async () => {
    stub();
    renderRoutes([{ path: "/", element: <GlobalFilterBar /> }], "/");

    expect(screen.queryByText("Filter active")).toBeNull();
    await screen.findByRole("option", { name: "Payment" });
    fireEvent.change(screen.getByLabelText("Project"), { target: { value: "payment" } });
    expect(screen.getByText("Filter active")).toBeTruthy();
    expect(globalFilterStore.get()).toEqual({ projectId: "payment" });

    fireEvent.click(screen.getByRole("button", { name: "Reset" }));
    expect(globalFilterStore.get()).toEqual({});
    expect(screen.queryByText("Filter active")).toBeNull();
  });

  it("drops a stored choice that no longer exists", async () => {
    globalFilterStore.set({ siteId: "gone", projectId: "payment" });
    stub();
    renderRoutes([{ path: "/", element: <GlobalFilterBar /> }], "/");

    await vi.waitFor(() => expect(globalFilterStore.get()).toEqual({ projectId: "payment" }));
  });
});
