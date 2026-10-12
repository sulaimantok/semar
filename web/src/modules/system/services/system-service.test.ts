import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/shared/api-client";

import { fetchReadiness, fetchVersion } from "./system-service";

function respond(status: number, body: unknown, contentType = "application/json") {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify(body), { status, headers: { "Content-Type": contentType } })),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("fetchReadiness", () => {
  it("reports ready on 200", async () => {
    respond(200, { status: "ready" });
    await expect(fetchReadiness()).resolves.toEqual({ ready: true });
  });

  it("treats a 503 problem as not ready", async () => {
    respond(
      503,
      { type: "about:blank", title: "Service Unavailable", status: 503, detail: "not ready: db" },
      "application/problem+json",
    );
    await expect(fetchReadiness()).resolves.toEqual({ ready: false, detail: "not ready: db" });
  });

  it("throws ApiError on other failures", async () => {
    respond(500, { type: "about:blank", title: "Internal Server Error", status: 500 }, "application/problem+json");
    await expect(fetchReadiness()).rejects.toBeInstanceOf(ApiError);
  });
});

describe("fetchVersion", () => {
  it("returns build information", async () => {
    const info = { version: "v0.1.0", commit: "abc", goVersion: "go1.26.9", platform: "linux/arm64" };
    respond(200, info);
    await expect(fetchVersion()).resolves.toEqual(info);
  });
});
