import { useQuery } from "@tanstack/react-query";

import { fetchReadiness } from "../services/system-service";

import { systemKeys } from "./system-keys";

export const READINESS_POLL_MS = 15_000;

export function useReadiness() {
  return useQuery({
    queryKey: systemKeys.readiness(),
    queryFn: fetchReadiness,
    refetchInterval: READINESS_POLL_MS,
  });
}
