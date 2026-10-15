import { afterEach, describe, expect, it } from "vitest";

import { globalFilterStore } from "./global-filter-store";

afterEach(() => {
  localStorage.clear();
  globalFilterStore.reload();
});

describe("globalFilterStore", () => {
  it("defaults to All and persists choices", () => {
    expect(globalFilterStore.get()).toEqual({});
    globalFilterStore.set({ siteId: "site-a" });
    globalFilterStore.set({ projectId: "payment" });
    expect(JSON.parse(localStorage.getItem("semar.global-filter.v1")!)).toEqual({ siteId: "site-a", projectId: "payment" });

    globalFilterStore.reload();
    expect(globalFilterStore.get()).toEqual({ siteId: "site-a", projectId: "payment" });
  });

  it("resets and ignores malformed storage", () => {
    globalFilterStore.set({ siteId: "site-a" });
    globalFilterStore.reset();
    expect(globalFilterStore.get()).toEqual({});

    localStorage.setItem("semar.global-filter.v1", '{"siteId": 7}');
    globalFilterStore.reload();
    expect(globalFilterStore.get()).toEqual({});
    localStorage.setItem("semar.global-filter.v1", "not json");
    globalFilterStore.reload();
    expect(globalFilterStore.get()).toEqual({});
  });

  it("notifies subscribers", () => {
    let calls = 0;
    const unsubscribe = globalFilterStore.subscribe(() => calls++);
    globalFilterStore.set({ projectId: "search" });
    unsubscribe();
    globalFilterStore.set({ projectId: "payment" });
    expect(calls).toBe(1);
  });
});
