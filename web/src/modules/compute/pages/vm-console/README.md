# VM console page

- **Route:** `/compute/vms/:providerId/:externalId/console/:kind` (`vnc` or `serial`)
- **Permissions:** an Admin or Operator binding covering the VM (the API answers 403 to viewers)
- **Data:** `useConsoleSession()` → `POST /api/v1/resources/{providerId}/vm/{externalId}/console` (single-use, 30 s token), then a WebSocket to `/api/v1/console/ws` with the subprotocols `binary` and `semar.token.<token>`
- **Rules:** the token is never put in a URL. noVNC and xterm.js attach while the socket is still connecting so no early bytes are lost. Close codes 4001–4004 are shown as messages (idle, maximum duration, access revoked, provider unavailable); **Reconnect** issues a new token.
