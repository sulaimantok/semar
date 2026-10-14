import { useInfiniteQuery } from "@tanstack/react-query";

import { fetchVms } from "../services/compute-service";

import { computeKeys } from "./compute-keys";

/** Cursor-paginated VM list from the inventory cache. */
export function useVmPages(providerId: string | undefined) {
  return useInfiniteQuery({
    queryKey: computeKeys.vmList(providerId),
    queryFn: ({ pageParam }) => fetchVms({ providerId, cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    // Inventory syncs every minute; refresh in the background.
    refetchInterval: 60_000,
  });
}
