import { useInfiniteQuery } from "@tanstack/react-query";

import { fetchVms, type IVmFilter } from "../services/compute-service";

import { computeKeys } from "./compute-keys";

/** Cursor-paginated VM list from the inventory cache. */
export function useVmPages(filter: IVmFilter) {
  return useInfiniteQuery({
    queryKey: computeKeys.vmList(filter),
    queryFn: ({ pageParam }) => fetchVms({ ...filter, cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    // Inventory syncs every minute; refresh in the background.
    refetchInterval: 60_000,
  });
}
