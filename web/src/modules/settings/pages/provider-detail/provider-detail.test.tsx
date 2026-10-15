import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { json, problem, renderRoutes, stubApi } from "@/shared/test/render";

import { ProviderDetailPage } from "./provider-detail";

const admin = { username: "admin", roles: [{ role: "admin" }] };
const operator = { username: "op", roles: [{ role: "operator" }] };
const provider = {
  id: "pve-lab",
  type: "proxmox",
  name: "PVE Lab",
  endpoints: ["https://pve1.example.com:8006"],
  regionName: "",
  fingerprint: "proxmox:abc",
  createdAt: "",
  updatedAt: "",
};
const credential = {
  id: "8f8c5a1e-4b7e-4c55-9d38-0c1f3f4c2b11",
  purpose: "discovery",
  scope: "provider",
  owner: "semar@pve",
  kekVersion: "k1",
  createdAt: new Date().toISOString(),
  rotatedAt: new Date().toISOString(),
};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function render(me: unknown, extra: Parameters<typeof stubApi>[0] = {}) {
  const calls = stubApi({
    "GET /api/v1/auth/me": () => json(200, me),
    "GET /api/v1/sites": () => json(200, { items: [] }),
    "GET /api/v1/providers/pve-lab": () => json(200, provider),
    "GET /api/v1/providers/pve-lab/sync-status": () =>
      json(200, {
        providerId: "pve-lab",
        status: "degraded",
        lastAttemptAt: new Date().toISOString(),
        consecutiveFailures: 1,
        lastError: "",
      }),
    "GET /api/v1/providers/pve-lab/credentials": () => json(200, { items: [credential] }),
    ...extra,
  });
  renderRoutes(
    [
      { path: "/settings/providers/:providerId", element: <ProviderDetailPage /> },
      { path: "/settings/providers", element: <p>providers list</p> },
    ],
    "/settings/providers/pve-lab",
  );
  return calls;
}

describe("ProviderDetailPage", () => {
  it("shows facts and sync status but no credentials to non-admins", async () => {
    const calls = render(operator);
    expect(await screen.findByText("proxmox:abc")).toBeTruthy();
    expect(await screen.findByText("degraded")).toBeTruthy();
    expect(screen.getByText(/Only global admins can view or manage credentials/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Remove provider" })).toBeNull();
    expect(calls.some((r) => new URL(r.url).pathname.endsWith("/credentials"))).toBe(false);
  });

  it("explains a provider that was never synced", async () => {
    render(admin, { "GET /api/v1/providers/pve-lab/sync-status": () => problem(404, "Not Found") });
    expect(await screen.findByText(/Not synced yet/)).toBeTruthy();
  });

  it("stores a write-only credential and clears the secret inputs", async () => {
    let sent: Record<string, unknown> | undefined;
    render(admin, {
      "PUT /api/v1/providers/pve-lab/credentials": async (req) => {
        sent = await req.json();
        return json(200, credential);
      },
    });

    const table = (await screen.findByText("semar@pve")).closest("table")!;
    expect(within(table).getByText("provider")).toBeTruthy();

    const tokenId = screen.getByLabelText("token_id") as HTMLInputElement;
    const tokenSecret = screen.getByLabelText("token_secret") as HTMLInputElement;
    expect(tokenSecret.type).toBe("password");
    fireEvent.change(tokenId, { target: { value: "semar@pve!discovery" } });
    fireEvent.change(tokenSecret, { target: { value: "s3cret" } });
    fireEvent.click(screen.getByRole("button", { name: "Store credential" }));

    expect(await screen.findByText(/Stored discovery credential/)).toBeTruthy();
    expect(sent).toEqual({
      purpose: "discovery",
      scope: "provider",
      secret: { token_id: "semar@pve!discovery", token_secret: "s3cret" },
    });
    expect(tokenSecret.value).toBe("");
    expect(screen.queryByDisplayValue("s3cret")).toBeNull();
  });

  it("removes the provider only after confirmation", async () => {
    const deleted = vi.fn(() => new Response(null, { status: 204 }));
    render(admin, { "DELETE /api/v1/providers/pve-lab": deleted });

    fireEvent.click(await screen.findByRole("button", { name: "Remove provider" }));
    expect(deleted).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Confirm remove" }));
    expect(await screen.findByText("providers list")).toBeTruthy();
    expect(deleted).toHaveBeenCalledOnce();
  });

  it("edits only the changed fields", async () => {
    let sent: unknown;
    render(admin, {
      "PATCH /api/v1/providers/pve-lab": async (req) => {
        sent = await req.json();
        return json(200, { ...provider, name: "PVE Lab (prod)" });
      },
    });

    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByLabelText(/^Name/), { target: { value: "PVE Lab (prod)" } });
    const save = screen.getByRole("button", { name: "Save changes" }) as HTMLButtonElement;
    expect(save.disabled).toBe(false);
    fireEvent.change(screen.getByLabelText(/^Endpoints/), { target: { value: "https://pve2.example.com:8006" } });
    expect(screen.getByText(/clears the last connection check/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText(/^Endpoints/), { target: { value: "https://pve1.example.com:8006\n" } });
    fireEvent.click(save);

    // The form closes on success; the page then refetches the provider.
    await vi.waitFor(() => expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull());
    expect(sent).toEqual({ name: "PVE Lab (prod)" });
  });

  it("checks the connection and shows the recorded identity", async () => {
    const validated = {
      ...provider,
      cloudFingerprint: "fake://seed-42",
      validatedAt: new Date().toISOString(),
      apiVersions: { compute: "fake-1.0" },
    };
    render(admin, { "POST /api/v1/providers/pve-lab/validate": () => json(200, validated) });

    expect(await screen.findByText(/Not checked since/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Check connection" }));
    expect(await screen.findByText("fake://seed-42")).toBeTruthy();
    expect(screen.getByText("compute fake-1.0")).toBeTruthy();
  });

  it("shows why a connection check failed", async () => {
    render(admin, {
      "POST /api/v1/providers/pve-lab/validate": () => problem(502, "Bad Gateway", "the provider refused the credentials"),
    });
    fireEvent.click(await screen.findByRole("button", { name: "Check connection" }));
    expect((await screen.findByRole("alert")).textContent).toMatch(/refused the credentials/);
  });

  it("hides editing and checks from non-admins", async () => {
    render(operator);
    expect(await screen.findByText(/Not checked since/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Check connection" })).toBeNull();
  });
});
