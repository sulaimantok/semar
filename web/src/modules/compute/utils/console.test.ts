import { describe, expect, it } from "vitest";

import { closeMessage, consoleProtocols, consoleSocketUrl } from "./console";

describe("console utils", () => {
  it("builds same-origin WebSocket URLs", () => {
    expect(consoleSocketUrl("/api/v1/console/ws", { protocol: "https:", host: "semar.example.com" })).toBe(
      "wss://semar.example.com/api/v1/console/ws",
    );
    expect(consoleSocketUrl("/api/v1/console/ws", { protocol: "http:", host: "localhost:5173" })).toBe(
      "ws://localhost:5173/api/v1/console/ws",
    );
  });

  it("carries the token as a subprotocol, never in the URL", () => {
    expect(consoleProtocols("abc")).toEqual(["binary", "semar.token.abc"]);
  });

  it("explains SEMAR close codes", () => {
    expect(closeMessage(4001)).toMatch(/inactivity/);
    expect(closeMessage(4003)).toMatch(/revoked/);
    expect(closeMessage(4999)).toMatch(/4999/);
  });
});
