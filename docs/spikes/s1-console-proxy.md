# Spike S1 — Server-Side Console Proxy for Nova and Proxmox

- **Date:** 2026-10-30 (lab results from 2026-10-18)
- **Related:** ADR-0005 (two-tier credentials), roadmap §3.4 (Console), security review [`v0.1.0-review.md`](../security/v0.1.0-review.md)
- **Outcome:** 🟡 Partial — the design holds for Nova VNC in a real cloud; Nova serial and both Proxmox consoles still need a lab run.

## Question

Can SEMAR open VNC and serial consoles for OpenStack (Nova `novncproxy` / `nova-serialproxy`) and Proxmox VE (`vncwebsocket` / `termproxy`) through its own WebSocket proxy, using the credentials planned in ADR-0005, so that no provider URL, token or ticket ever reaches the browser?

## Design Under Test

| Step | Nova | Proxmox VE |
|---|---|---|
| Ask for a console | `POST /servers/{id}/remote-consoles` (microversion ≥ 2.6), `vnc`/`novnc` or `serial`/`serial` | `POST /nodes/{node}/qemu/{vmid}/vncproxy` (`websocket=1`) or `termproxy` |
| What comes back | URL with a token for the console proxy, reusable until `[consoleauth] token_ttl` | `port` + `ticket` (valid for two hours) |
| Upstream connection | WebSocket to the console proxy, `Origin` rewritten to the proxy's own host | WebSocket to `vncwebsocket?port=…&vncticket=…` with `Authorization: PVEAPIToken=…` |
| RFB / serial auth | none (token in the URL) | VNC: the ticket is the RFB password, answered by SEMAR; serial: `user:ticket\n` handshake, then termproxy framing |
| Required privilege | project `member` (or admin) on the VM's project | `VM.Console` on the VM |
| Credential used | `mutate` for the VM's scope, falling back to `discovery` | same |

The browser only receives a SEMAR console token (single-use, ≤ 30 s, bound to user + VM + session, sent as a WebSocket subprotocol). The provider side is opened when the browser connects.

## Findings

### 1. Nova VNC — ✅ verified in a real cloud

- Lab: OpenStack (Atmosphere) with an ARM64 compute node; compute API microversion 2.100.
- Consoles of two ARM64 VMs opened through SEMAR; the QEMU framebuffer (1280×800) rendered in noVNC.
- The browser connected only to SEMAR and never authenticated to the cloud. In the end-to-end runs against the Lite stack, a reused SEMAR token and a foreign `Origin` were refused with 403, and the token never appeared in server logs.
- `novncproxy` checks `Origin`; rewriting it to the proxy's own host in the server-side dial was enough. No `allowed_origins` change was needed in the cloud.
- A client-side bug (the viewer was remounted and closed the socket) looked like a provider failure at first. It is fixed and covered by a test that counts mounts.

### 2. Nova serial — ⛔ blocked by cloud configuration

- Nova answers `400 Unavailable console type serial.` because the serial console proxy is not enabled in the lab cloud.
- SEMAR now maps this response to "console type not available" and shows the reason in the UI instead of a generic error.
- Testing needs `[serial_console] enabled = true` on the compute nodes and a running `nova-serialproxy` (Kolla: `enable_nova_serialconsole_proxy`).

### 3. Proxmox VNC and serial — 🧪 verified against a fake API only

- `vncproxy` → `vncwebsocket` with the token header, RFB auth relayed by SEMAR, and `termproxy` framing pass in the driver and proxy tests and in an end-to-end run against an in-process fake PVE API.
- Not yet confirmed on a real PVE: whether `vncwebsocket` accepts an API token (not a cookie ticket) on the lab's PVE version, and whether `port` is a number or a string.

### 4. Credentials

- A project-scoped **reader** cannot create Nova consoles (`os_compute_api:os-remote-consoles` defaults to `project_member_or_admin`), and `PVEAuditor` has no `VM.Console`. Consoles therefore use the `mutate` credential of the VM's scope and fall back to `discovery` only where that credential happens to be allowed.
- The lab run used an application credential that is allowed to create consoles on the VM's project.

## Decisions

1. Keep the server-side proxy design; no provider URL or ticket is sent to the browser.
2. Console credentials are `mutate`, not `discovery` (already in roadmap §3.4).
3. "Console type not available" is a normal outcome, reported per provider, not an error.

## Open Items

- Enable the Nova serial console proxy in the lab cloud and re-run the serial test.
- Run VNC and serial against the Proxmox lab with an API token that has `VM.Console`, and record the PVE version.
- Record per console session that an audit `open`/`close` pair exists for both providers (Phase 1 exit criterion).
