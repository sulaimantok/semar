import RFB from "@novnc/novnc";
import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";

import { vncScreenStyles as styles } from "./vnc-screen.styles";

export interface IVncScreenHandle {
  sendCtrlAltDel(): void;
}

export interface IVncScreenProps {
  /** A WebSocket to the SEMAR console endpoint, still connecting: noVNC must
   * attach before the server's first bytes arrive. */
  socket: WebSocket;
  onConnected: () => void;
}

/** Renders a VNC console with noVNC over an already opened WebSocket. */
export const VncScreen = forwardRef<IVncScreenHandle, IVncScreenProps>(function VncScreen({ socket, onConnected }, ref) {
  const host = useRef<HTMLDivElement>(null);
  const rfb = useRef<RFB | null>(null);

  useImperativeHandle(ref, () => ({ sendCtrlAltDel: () => rfb.current?.sendCtrlAltDel() }), []);

  useEffect(() => {
    if (!host.current) return;
    const client = new RFB(host.current, socket);
    client.scaleViewport = true;
    client.background = "#000";
    client.addEventListener("connect", onConnected);
    rfb.current = client;
    return () => {
      client.removeEventListener("connect", onConnected);
      rfb.current = null;
      client.disconnect();
    };
  }, [socket, onConnected]);

  return <div ref={host} className={styles.screen} aria-label="VNC console" tabIndex={-1} />;
});
