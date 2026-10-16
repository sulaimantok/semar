// Pages
// Loaded on demand (noVNC, xterm.js); render inside <Suspense>.
export { VmConsolePageLazy as VmConsolePage } from "./pages/vm-console/lazy";
export { VmDetailPage } from "./pages/vm-detail";
export { VmListPage } from "./pages/vm-list";

// Components
export { CapacityOverview } from "./components/capacity-overview";

// Hooks
export { computeKeys } from "./hooks/compute-keys";

// Types
export type { ICapacity, IProvider, IResource, IResourceDetail } from "./services/compute-service";
