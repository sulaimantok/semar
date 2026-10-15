import { useQuery } from "@tanstack/react-query";

import {
  fetchCredentials,
  fetchMappings,
  fetchProjects,
  fetchProvider,
  fetchProviders,
  fetchSites,
  fetchSyncStatus,
} from "../services/settings-service";

import { settingsKeys } from "./settings-keys";

export function useSites() {
  return useQuery({ queryKey: settingsKeys.sites(), queryFn: fetchSites });
}

export function useProjects() {
  return useQuery({ queryKey: settingsKeys.projects(), queryFn: fetchProjects });
}

export function useMappings(projectId: string, enabled = true) {
  return useQuery({ queryKey: settingsKeys.mappings(projectId), queryFn: () => fetchMappings(projectId), enabled });
}

export function useSettingsProviders() {
  return useQuery({ queryKey: settingsKeys.providers(), queryFn: fetchProviders });
}

export function useProvider(providerId: string) {
  return useQuery({ queryKey: settingsKeys.provider(providerId), queryFn: () => fetchProvider(providerId) });
}

export function useSyncStatus(providerId: string) {
  return useQuery({
    queryKey: settingsKeys.syncStatus(providerId),
    queryFn: () => fetchSyncStatus(providerId),
    refetchInterval: 30_000,
  });
}

/** Credential metadata; admin-only, so callers pass enabled = isAdmin. */
export function useCredentials(providerId: string, enabled: boolean) {
  return useQuery({
    queryKey: settingsKeys.credentials(providerId),
    queryFn: () => fetchCredentials(providerId),
    enabled,
  });
}
