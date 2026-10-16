import { describe, expect, it } from "vitest";

import { canOperate, isGlobalAdmin } from "./roles";

describe("isGlobalAdmin", () => {
  it("accepts only unscoped admin bindings", () => {
    expect(isGlobalAdmin({ username: "a", roles: [{ role: "admin" }] })).toBe(true);
    expect(isGlobalAdmin({ username: "b", roles: [{ role: "admin", projectId: "payment" }] })).toBe(false);
    expect(isGlobalAdmin({ username: "c", roles: [{ role: "admin", providerId: "pve-lab" }] })).toBe(false);
    expect(isGlobalAdmin({ username: "d", roles: [{ role: "operator" }] })).toBe(false);
    expect(isGlobalAdmin(null)).toBe(false);
  });
});

describe("canOperate", () => {
  const vm = { projectId: "payment", providerId: "pve-lab" };
  it("matches the API's console rule", () => {
    expect(canOperate({ username: "a", roles: [{ role: "operator" }] }, vm)).toBe(true);
    expect(canOperate({ username: "a", roles: [{ role: "operator" }] }, { providerId: "pve-lab" })).toBe(true);
    expect(canOperate({ username: "b", roles: [{ role: "admin", projectId: "payment" }] }, vm)).toBe(true);
    expect(canOperate({ username: "c", roles: [{ role: "operator", providerId: "pve-lab" }] }, vm)).toBe(true);
    expect(canOperate({ username: "c", roles: [{ role: "operator", providerId: "pve-lab" }] }, { providerId: "pve-lab" })).toBe(false);
    expect(canOperate({ username: "d", roles: [{ role: "viewer" }] }, vm)).toBe(false);
    expect(canOperate({ username: "e", roles: [{ role: "operator", projectId: "search" }] }, vm)).toBe(false);
    expect(canOperate(null, vm)).toBe(false);
  });
});
