import { lazy } from "react";

/**
 * The console page pulls in noVNC and xterm.js; loading it on demand keeps
 * them out of the main bundle. Render inside <Suspense>.
 */
export const VmConsolePageLazy = lazy(() => import("./vm-console").then((m) => ({ default: m.VmConsolePage })));
