import { describe, expect, it } from "vitest";

import { parseLabels } from "./labels";

describe("parseLabels", () => {
  it("parses comma-separated pairs", () => {
    expect(parseLabels("env=prod, tier = 1,")).toEqual({ env: "prod", tier: "1" });
    expect(parseLabels("")).toEqual({});
  });

  it("rejects pairs without a key", () => {
    expect(parseLabels("env")).toBeNull();
    expect(parseLabels("=prod")).toBeNull();
  });
});
