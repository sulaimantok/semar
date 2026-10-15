import { useQuery } from "@tanstack/react-query";

import { fetchProjectOptions, fetchSiteOptions } from "../services/filter-options-service";

import { globalFilterKeys } from "./global-filter-keys";

export function useSiteOptions() {
  return useQuery({ queryKey: globalFilterKeys.sites(), queryFn: fetchSiteOptions, staleTime: 60_000 });
}

export function useProjectOptions() {
  return useQuery({ queryKey: globalFilterKeys.projects(), queryFn: fetchProjectOptions, staleTime: 60_000 });
}
