// Minimal typing for the parts of noVNC 1.7 SEMAR uses. @types/novnc__novnc
// still describes the pre-1.6 "lib/rfb" path, while the package now exports
// "core/rfb.js" as its entry point.
declare module "@novnc/novnc" {
  export interface IRFBOptions {
    wsProtocols?: string[];
    shared?: boolean;
  }

  export default class RFB extends EventTarget {
    constructor(target: HTMLElement, urlOrChannel: string | WebSocket, options?: IRFBOptions);
    scaleViewport: boolean;
    resizeSession: boolean;
    clipViewport: boolean;
    background: string;
    disconnect(): void;
    sendCtrlAltDel(): void;
    focus(): void;
  }
}
