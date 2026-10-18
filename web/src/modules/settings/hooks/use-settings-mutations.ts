import { useMutation, useQueryClient } from "@tanstack/react-query";

import { computeKeys } from "@/modules/compute";
import { globalFilterKeys } from "@/modules/global-filter";

import {
  createMapping,
  deleteMapping,
  updateMapping,
  createProject,
  createProvider,
  createSite,
  deleteCredential,
  deleteProvider,
  updateProvider,
  validateProvider,
  putCredential,
  type ICredentialPurpose,
  type ICredentialPut,
  type IProjectMappingCreate,
  type IProjectMappingUpdate,
  type IProjectCreate,
  type IProviderCreate,
  type IProviderUpdate,
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

/** Mapping changes re-derive ownership on the next sync. */
function useInvalidateMappings(projectId: string) {
  const qc = useQueryClient();
  return async () => {
    await qc.invalidateQueries({ queryKey: settingsKeys.mappings(projectId) });
    await qc.invalidateQueries({ queryKey: computeKeys.all });
  };
}

export function useUpdateMapping(projectId: string) {
  const invalidate = useInvalidateMappings(projectId);
  return useMutation({
    mutationFn: ({ id, body }: { id: number; body: IProjectMappingUpdate }) => updateMapping(projectId, id, body),
    onSuccess: invalidate,
  });
}

export function useDeleteMapping(projectId: string) {
  const invalidate = useInvalidateMappings(projectId);
  return useMutation({ mutationFn: (id: number) => deleteMapping(projectId, id), onSuccess: invalidate });
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

export function useUpdateProvider(providerId: string) {
  const qc = useQueryClient();
  const invalidate = useInvalidateProviders();
  return useMutation({
    mutationFn: (body: IProviderUpdate) => updateProvider(providerId, body),
    onSuccess: async (p) => {
      qc.setQueryData(settingsKeys.provider(providerId), p);
      await invalidate();
    },
  });
}

export function useValidateProvider(providerId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => validateProvider(providerId),
    onSuccess: (p) => qc.setQueryData(settingsKeys.provider(providerId), p),
  });
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
