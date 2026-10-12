import { useQuery } from "@tanstack/react-query";

import { fetchVersion } from "../services/system-service";

import { systemKeys } from "./system-keys";

export function useVersion() {
  return useQuery({
    queryKey: systemKeys.version(),
    queryFn: fetchVersion,
    staleTime: 5 * 60_000,
  });
}
