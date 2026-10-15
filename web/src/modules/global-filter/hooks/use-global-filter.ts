import { useSyncExternalStore } from "react";

import { globalFilterStore, type IGlobalFilter } from "../stores/global-filter-store";

/** The active global filter; re-renders when it changes. */
export function useGlobalFilter(): IGlobalFilter & {
  isActive: boolean;
  set: (patch: IGlobalFilter) => void;
  reset: () => void;
} {
  const f = useSyncExternalStore(globalFilterStore.subscribe, globalFilterStore.get, globalFilterStore.get);
  return { ...f, isActive: !!(f.siteId || f.projectId), set: globalFilterStore.set, reset: globalFilterStore.reset };
}
