/** WebSocket URL of a console path on the current origin. */
export function consoleSocketUrl(path: string, loc: Pick<Location, "protocol" | "host"> = window.location): string {
  return `${loc.protocol === "https:" ? "wss" : "ws"}://${loc.host}${path}`;
}

/**
 * Subprotocols for a console connection: the token travels as a
 * subprotocol so it never appears in URLs, proxy logs or history.
 */
export function consoleProtocols(token: string): string[] {
  return ["binary", `semar.token.${token}`];
}

/** Human message for a console close code (see the API's console docs). */
export function closeMessage(code: number): string {
  switch (code) {
    case 1000:
    case 1005:
      return "Console closed.";
    case 4001:
      return "Closed after inactivity.";
    case 4002:
      return "The maximum console duration was reached.";
    case 4003:
      return "Your access to this console was revoked or your session ended.";
    case 4004:
      return "The provider console is not available.";
    case 1006:
      return "The connection was lost.";
    default:
      return `Console closed (code ${code}).`;
  }
}
