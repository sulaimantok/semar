import { describe, expect, it } from "vitest";

import { ApiError, isProblem } from "./problem";

describe("isProblem", () => {
  it("accepts RFC 9457 problem bodies", () => {
    expect(isProblem({ type: "about:blank", title: "Not Found", status: 404 })).toBe(true);
  });

  it("rejects other values", () => {
    expect(isProblem(null)).toBe(false);
    expect(isProblem("oops")).toBe(false);
    expect(isProblem({ status: "404" })).toBe(false);
  });
});

describe("ApiError", () => {
  it("prefers the problem detail and exposes the request ID", () => {
    const err = new ApiError(503, {
      type: "about:blank",
      title: "Service Unavailable",
      status: 503,
      detail: "not ready: db",
      requestId: "req-1",
    });
    expect(err.message).toBe("not ready: db");
    expect(err.requestId).toBe("req-1");
  });

  it("falls back to the status when there is no problem body", () => {
    expect(new ApiError(502).message).toBe("Request failed with status 502");
  });
});
