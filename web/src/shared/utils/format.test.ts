import { describe, expect, it } from "vitest";

import { formatAge, formatMemoryMiB } from "./format";

const now = new Date("2026-10-14T10:00:00Z");

describe("formatAge", () => {
  it.each([
    ["2026-10-14T09:59:20Z", "40 s ago"],
    ["2026-10-14T09:57:00Z", "3 min ago"],
    ["2026-10-14T07:00:00Z", "3 h ago"],
    ["2026-10-10T10:00:00Z", "4 d ago"],
    ["2026-10-14T10:00:30Z", "0 s ago"],
  ])("%s → %s", (at, want) => {
    expect(formatAge(at, now)).toBe(want);
  });
});

describe("formatMemoryMiB", () => {
  it.each([
    [512, "512 MiB"],
    [4096, "4 GiB"],
    [1536, "1.5 GiB"],
  ])("%d → %s", (mib, want) => {
    expect(formatMemoryMiB(mib)).toBe(want);
  });
});
