import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { json, problem, renderRoutes, stubApi } from "@/shared/test/render";

import { VmConsolePage } from "./vm-console";

// noVNC and xterm.js need a real canvas; the page logic is what is tested.
vi.mock("./components/vnc-screen", () => ({ VncScreen: () => <div>vnc screen</div> }));
vi.mock("./components/serial-terminal", () => ({ SerialTerminal: () => <div>serial terminal</div> }));

class FakeSocket extends EventTarget {
  static last: FakeSocket | undefined;
  binaryType = "blob";
  readyState = 0;
  constructor(
    public url: string,
    public protocols: string[],
  ) {
    super();
    FakeSocket.last = this;
  }
  close() {}
  send() {}
}

beforeEach(() => vi.stubGlobal("WebSocket", FakeSocket));
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  FakeSocket.last = undefined;
});

const vm = {
  resource: {
    providerId: "pve-lab", kind: "vm", externalId: "100", name: "web-1", externalScope: "", arch: "arm64",
    status: "running", spec: {}, observedAt: new Date().toISOString(), firstSeenAt: new Date().toISOString(),
  },
  changes: [],
};

function render(kind: string, consoleAnswer: () => Response) {
  const calls = stubApi({
    "GET /api/v1/resources/pve-lab/vm/100": () => json(200, vm),
    "POST /api/v1/resources/pve-lab/vm/100/console": consoleAnswer,
  });
  renderRoutes([{ path: "/compute/vms/:providerId/:externalId/console/:kind", element: <VmConsolePage /> }], `/compute/vms/pve-lab/100/console/${kind}`);
  return calls;
}

describe("VmConsolePage", () => {
  it("connects with the token as a subprotocol, never in the URL", async () => {
    const calls = render("vnc", () =>
      json(201, { token: "tok123", kind: "vnc", expiresAt: new Date().toISOString(), websocketPath: "/api/v1/console/ws" }),
    );
    expect(await screen.findByText("vnc screen")).toBeTruthy();
    const ws = FakeSocket.last!;
    expect(ws.url).toBe(`ws://${window.location.host}/api/v1/console/ws`);
    expect(ws.protocols).toEqual(["binary", "semar.token.tok123"]);
    expect(ws.url).not.toContain("tok123");
    const post = calls.find((c) => c.method === "POST")!;
    expect(await post.clone().json()).toEqual({ kind: "vnc" });
  });

  it("explains close codes and offers a reconnect", async () => {
    render("serial", () =>
      json(201, { token: "t", kind: "serial", expiresAt: new Date().toISOString(), websocketPath: "/api/v1/console/ws" }),
    );
    expect(await screen.findByText("serial terminal")).toBeTruthy();
    FakeSocket.last!.dispatchEvent(Object.assign(new Event("close"), { code: 4001 }));
    expect(await screen.findByText(/Closed after inactivity/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Reconnect" })).toBeTruthy();
  });

  it("tells viewers why they cannot open a console", async () => {
    render("vnc", () => problem(403, "Forbidden", "consoles require an Admin or Operator role for this VM"));
    expect(await screen.findByText(/require an Admin or Operator role/)).toBeTruthy();
    expect(FakeSocket.last).toBeUndefined();
  });
});
