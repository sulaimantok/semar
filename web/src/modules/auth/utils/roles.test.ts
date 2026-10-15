import { describe, expect, it } from "vitest";

import { isGlobalAdmin } from "./roles";

describe("isGlobalAdmin", () => {
  it("accepts only unscoped admin bindings", () => {
    expect(isGlobalAdmin({ username: "a", roles: [{ role: "admin" }] })).toBe(true);
    expect(isGlobalAdmin({ username: "b", roles: [{ role: "admin", projectId: "payment" }] })).toBe(false);
    expect(isGlobalAdmin({ username: "c", roles: [{ role: "admin", providerId: "pve-lab" }] })).toBe(false);
    expect(isGlobalAdmin({ username: "d", roles: [{ role: "operator" }] })).toBe(false);
    expect(isGlobalAdmin(null)).toBe(false);
  });
});
