import { useQuery } from "@tanstack/react-query";

import { fetchProviders } from "../services/compute-service";

import { computeKeys } from "./compute-keys";

export function useProviders() {
  return useQuery({ queryKey: computeKeys.providers(), queryFn: fetchProviders, staleTime: 5 * 60_000 });
}
