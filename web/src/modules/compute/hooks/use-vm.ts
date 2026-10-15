import { useQuery } from "@tanstack/react-query";

import { fetchVm } from "../services/compute-service";

import { computeKeys } from "./compute-keys";

export function useVm(providerId: string, externalId: string) {
  return useQuery({
    queryKey: computeKeys.vm(providerId, externalId),
    queryFn: () => fetchVm(providerId, externalId),
    refetchInterval: 60_000,
  });
}
