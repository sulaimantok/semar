import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router";

import { ApiError } from "@/shared/api-client";
import { cn } from "@/shared/utils/cn";

import { useConsoleSession } from "../../hooks/use-console-session";
import { useVm } from "../../hooks/use-vm";
import type { IConsoleKind } from "../../services/compute-service";
import { closeMessage, consoleProtocols, consoleSocketUrl } from "../../utils/console";
import { vmPath } from "../../utils/vm-path";

import { SerialTerminal } from "./components/serial-terminal";
import { VncScreen, type IVncScreenHandle } from "./components/vnc-screen";
import { vmConsoleStyles as styles } from "./vm-console.styles";

type IPhase = "connecting" | "open" | "closed";

function tokenError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 403) return "Consoles require an Admin or Operator role for this VM.";
    if (err.status === 404) return "This virtual machine does not exist or is outside the projects you can see.";
    if (err.status === 409) return "The VM is no longer reported by its provider.";
    return err.message;
  }
  return "Cannot reach the SEMAR API.";
}

/**
 * VNC (noVNC) or serial (xterm.js) console of one VM. The page asks the
 * API for a single-use token and connects to SEMAR's console proxy; the
 * provider's URL and ticket never reach the browser.
 */
export function VmConsolePage() {
  const { providerId = "", externalId = "", kind: kindParam = "vnc" } = useParams();
  const kind: IConsoleKind = kindParam === "serial" ? "serial" : "vnc";
  const vm = useVm(providerId, externalId);
  const session = useConsoleSession(providerId, externalId);
  const [socket, setSocket] = useState<WebSocket | null>(null);
  const [phase, setPhase] = useState<IPhase>("connecting");
  const [closed, setClosed] = useState("");
  const vnc = useRef<IVncScreenHandle>(null);
  const { mutate } = session;

  const connect = useCallback(() => {
    setPhase("connecting");
    setClosed("");
    mutate(kind, {
      onSuccess: (s) => {
        const ws = new WebSocket(consoleSocketUrl(s.websocketPath), consoleProtocols(s.token));
        ws.binaryType = "arraybuffer";
        ws.addEventListener("open", () => setPhase("open"));
        ws.addEventListener("close", (e) => {
          setPhase("closed");
          setClosed(closeMessage(e.code));
        });
        setSocket(ws);
      },
      onError: () => setPhase("closed"),
    });
  }, [kind, mutate]);

  useEffect(() => {
    connect();
  }, [connect]);
  useEffect(() => () => socket?.close(1000), [socket]);

  const onVncConnected = useCallback(() => setPhase("open"), []);
  const name = vm.data?.resource.name || externalId;
  const base = vmPath({ providerId, externalId });

  return (
    <div className={styles.page}>
      <div className={styles.toolbar}>
        <div className={styles.titleGroup}>
          <Link className={styles.back} to={base}>
            ← {name}
          </Link>
          <h1 className={styles.title}>{kind === "vnc" ? "VNC console" : "Serial console"}</h1>
        </div>
        <div className={styles.actions}>
          <span className={styles.status} role="status">
            <span
              className={cn(styles.dot, phase === "open" ? styles.dotOk : phase === "connecting" ? styles.dotWait : styles.dotClosed)}
              aria-hidden="true"
            />
            {phase === "open" ? "Connected" : phase === "connecting" ? "Connecting…" : "Disconnected"}
          </span>
          <Link className={cn(styles.tab, kind === "vnc" && styles.tabActive)} to={`${base}/console/vnc`}>
            VNC
          </Link>
          <Link className={cn(styles.tab, kind === "serial" && styles.tabActive)} to={`${base}/console/serial`}>
            Serial
          </Link>
          {kind === "vnc" && (
            <button type="button" className={styles.button} disabled={phase !== "open"} onClick={() => vnc.current?.sendCtrlAltDel()}>
              Ctrl+Alt+Del
            </button>
          )}
          {phase === "closed" && (
            <button type="button" className={styles.button} onClick={connect}>
              Reconnect
            </button>
          )}
        </div>
      </div>

      {session.isError && (
        <p className={styles.error} role="alert">
          {tokenError(session.error)}
        </p>
      )}
      {closed && !session.isError && (
        <p className={styles.note} role="alert">
          {closed}
        </p>
      )}

      {socket &&
        (kind === "vnc" ? (
          <VncScreen key={`vnc-${socket.url}-${session.data?.token}`} ref={vnc} socket={socket} onConnected={onVncConnected} />
        ) : (
          <SerialTerminal key={`serial-${session.data?.token}`} socket={socket} />
        ))}
      <p className={styles.note}>
        Sessions close after inactivity and after a maximum duration; every session is recorded in the audit log.
      </p>
    </div>
  );
}
