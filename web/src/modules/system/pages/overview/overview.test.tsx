import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { OverviewPage } from "./overview";

const json = (status: number, body: unknown, type = "application/json") =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": type } });

function stubApi(readyz: Response) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const path = new URL(req.url).pathname;
      if (path === "/readyz") return readyz.clone();
      if (path === "/api/v1/version") {
        return json(200, { version: "v0.1.0", commit: "abc1234", goVersion: "go1.26.9", platform: "linux/arm64" });
      }
      if (path === "/api/v1/capacity") {
        return json(200, { vms: { total: 0, vcpus: 0, memoryMiB: 0, byStatus: [], byArch: [] } });
      }
      return json(404, { type: "about:blank", title: "Not Found", status: 404 }, "application/problem+json");
    }),
  );
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <OverviewPage />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("OverviewPage", () => {
  it("shows a ready API and its build", async () => {
    stubApi(json(200, { status: "ready" }));
    renderPage();
    expect(await screen.findByText("Ready")).toBeTruthy();
    expect(await screen.findByText("v0.1.0")).toBeTruthy();
    expect(screen.getByText("linux/arm64")).toBeTruthy();
    expect(await screen.findByText(/No inventory yet/)).toBeTruthy();
  });

  it("shows failing readiness checks", async () => {
    stubApi(
      json(503, { type: "about:blank", title: "Service Unavailable", status: 503, detail: "not ready: db" }, "application/problem+json"),
    );
    renderPage();
    expect(await screen.findByText("Not ready")).toBeTruthy();
    expect(screen.getByText("not ready: db")).toBeTruthy();
  });
});
