import { useMutation, useQueryClient } from "@tanstack/react-query";

import { computeKeys } from "@/modules/compute";
import { globalFilterKeys } from "@/modules/global-filter";

import {
  createMapping,
  createProject,
  createProvider,
  createSite,
  deleteCredential,
  deleteProvider,
  putCredential,
  type ICredentialPurpose,
  type ICredentialPut,
  type IProjectMappingCreate,
  type IProjectCreate,
  type IProviderCreate,
  type ISiteCreate,
} from "../services/settings-service";

import { settingsKeys } from "./settings-keys";

export function useCreateSite() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: ISiteCreate) => createSite(body),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: settingsKeys.sites() });
      await qc.invalidateQueries({ queryKey: globalFilterKeys.sites() });
    },
  });
}

export function useCreateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: IProjectCreate) => createProject(body),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: settingsKeys.projects() });
      await qc.invalidateQueries({ queryKey: globalFilterKeys.projects() });
    },
  });
}

export function useCreateMapping(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: IProjectMappingCreate) => createMapping(projectId, body),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: settingsKeys.mappings(projectId) });
      // Ownership is re-derived on the next sync; refresh inventory views.
      await qc.invalidateQueries({ queryKey: computeKeys.all });
    },
  });
}

/** Provider changes also affect the compute module's provider filter. */
function useInvalidateProviders() {
  const qc = useQueryClient();
  return async () => {
    await qc.invalidateQueries({ queryKey: settingsKeys.providers() });
    await qc.invalidateQueries({ queryKey: computeKeys.all });
  };
}

export function useCreateProvider() {
  const invalidate = useInvalidateProviders();
  return useMutation({ mutationFn: (body: IProviderCreate) => createProvider(body), onSuccess: invalidate });
}

export function useDeleteProvider() {
  const invalidate = useInvalidateProviders();
  return useMutation({ mutationFn: (providerId: string) => deleteProvider(providerId), onSuccess: invalidate });
}

export function usePutCredential(providerId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: ICredentialPut) => putCredential(providerId, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: settingsKeys.credentials(providerId) }),
  });
}

export function useDeleteCredential(providerId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ purpose, scope }: { purpose: ICredentialPurpose; scope: string }) =>
      deleteCredential(providerId, purpose, scope),
    onSuccess: () => qc.invalidateQueries({ queryKey: settingsKeys.credentials(providerId) }),
  });
}
