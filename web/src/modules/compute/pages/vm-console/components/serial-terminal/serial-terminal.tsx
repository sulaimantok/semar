import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useEffect, useRef } from "react";

import { serialTerminalStyles as styles } from "./serial-terminal.styles";

export interface ISerialTerminalProps {
  /** A WebSocket to the SEMAR console endpoint, connecting or open. */
  socket: WebSocket;
}

/**
 * A serial console in xterm.js. Keystrokes go out as binary messages,
 * terminal size changes as {"cols","rows"} text messages.
 */
export function SerialTerminal({ socket }: ISerialTerminalProps) {
  const host = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!host.current) return;
    const term = new Terminal({ cursorBlink: true, convertEol: false, fontSize: 14 });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host.current);
    fit.fit();
    term.focus();

    socket.binaryType = "arraybuffer";
    const encoder = new TextEncoder();
    const onMessage = (e: MessageEvent) => {
      term.write(typeof e.data === "string" ? e.data : new Uint8Array(e.data as ArrayBuffer));
    };
    socket.addEventListener("message", onMessage);
    const input = term.onData((d) => socket.readyState === WebSocket.OPEN && socket.send(encoder.encode(d)));
    const sendSize = () =>
      socket.readyState === WebSocket.OPEN && socket.send(JSON.stringify({ cols: term.cols, rows: term.rows }));
    const resize = term.onResize(sendSize);
    // Mounted while the socket is still connecting, so that no early output
    // is missed; the size is sent once it opens.
    sendSize();
    socket.addEventListener("open", sendSize);
    const onWindowResize = () => fit.fit();
    window.addEventListener("resize", onWindowResize);

    return () => {
      window.removeEventListener("resize", onWindowResize);
      socket.removeEventListener("open", sendSize);
      socket.removeEventListener("message", onMessage);
      input.dispose();
      resize.dispose();
      term.dispose();
    };
  }, [socket]);

  return <div ref={host} className={styles.terminal} aria-label="Serial console" />;
}
