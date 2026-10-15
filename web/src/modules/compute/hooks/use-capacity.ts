import { useQuery } from "@tanstack/react-query";

import { fetchCapacity, type ICapacityQuery } from "../services/compute-service";

import { computeKeys } from "./compute-keys";

// Aggregates follow the inventory cache, which syncs about once a minute.
export function useCapacity(query: ICapacityQuery = {}) {
  return useQuery({
    queryKey: computeKeys.capacity(query),
    queryFn: () => fetchCapacity(query),
    staleTime: 30_000,
    refetchInterval: 60_000,
  });
}
