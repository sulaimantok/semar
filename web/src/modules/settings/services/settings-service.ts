import { ApiError, apiClient, isProblem, type components } from "@/shared/api-client";

export type ISite = components["schemas"]["Site"];
export type ISiteCreate = components["schemas"]["SiteCreate"];
export type IProject = components["schemas"]["Project"];
export type IProjectCreate = components["schemas"]["ProjectCreate"];
export type IProjectMapping = components["schemas"]["ProjectMapping"];
export type IProjectMappingCreate = components["schemas"]["ProjectMappingCreate"];
export type IProvider = components["schemas"]["Provider"];
export type IProviderCreate = components["schemas"]["ProviderCreate"];
export type IProviderType = components["schemas"]["ProviderType"];
export type ISyncStatus = components["schemas"]["SyncStatus"];
export type ICredential = components["schemas"]["Credential"];
export type ICredentialPut = components["schemas"]["CredentialPut"];
export type ICredentialPurpose = components["schemas"]["CredentialPurpose"];

/** Unwraps an openapi-fetch result or throws the problem as ApiError. */
function unwrap<T>(r: { data?: T; error?: unknown; response: Response }): T {
  if (r.data !== undefined) return r.data;
  throw new ApiError(r.response.status, isProblem(r.error) ? r.error : undefined);
}

function ensureOk(r: { error?: unknown; response: Response }): void {
  if (!r.response.ok) throw new ApiError(r.response.status, isProblem(r.error) ? r.error : undefined);
}

export async function fetchSites(): Promise<ISite[]> {
  return unwrap(await apiClient.GET("/api/v1/sites")).items;
}

export async function createSite(body: ISiteCreate): Promise<ISite> {
  return unwrap(await apiClient.POST("/api/v1/sites", { body }));
}

export async function fetchProjects(): Promise<IProject[]> {
  return unwrap(await apiClient.GET("/api/v1/projects")).items;
}

export async function createProject(body: IProjectCreate): Promise<IProject> {
  return unwrap(await apiClient.POST("/api/v1/projects", { body }));
}

export async function fetchMappings(projectId: string): Promise<IProjectMapping[]> {
  return unwrap(await apiClient.GET("/api/v1/projects/{projectId}/mappings", { params: { path: { projectId } } })).items;
}

export async function createMapping(projectId: string, body: IProjectMappingCreate): Promise<IProjectMapping> {
  return unwrap(await apiClient.POST("/api/v1/projects/{projectId}/mappings", { params: { path: { projectId } }, body }));
}

export async function fetchProviders(): Promise<IProvider[]> {
  return unwrap(await apiClient.GET("/api/v1/providers")).items;
}

export async function fetchProvider(providerId: string): Promise<IProvider> {
  return unwrap(await apiClient.GET("/api/v1/providers/{providerId}", { params: { path: { providerId } } }));
}

export async function createProvider(body: IProviderCreate): Promise<IProvider> {
  return unwrap(await apiClient.POST("/api/v1/providers", { body }));
}

export async function deleteProvider(providerId: string): Promise<void> {
  ensureOk(await apiClient.DELETE("/api/v1/providers/{providerId}", { params: { path: { providerId } } }));
}

/** The provider's last sync, or null before its first sync attempt. */
export async function fetchSyncStatus(providerId: string): Promise<ISyncStatus | null> {
  const r = await apiClient.GET("/api/v1/providers/{providerId}/sync-status", { params: { path: { providerId } } });
  if (r.response.status === 404) return null;
  return unwrap(r);
}

export async function fetchCredentials(providerId: string): Promise<ICredential[]> {
  return unwrap(await apiClient.GET("/api/v1/providers/{providerId}/credentials", { params: { path: { providerId } } }))
    .items;
}

export async function putCredential(providerId: string, body: ICredentialPut): Promise<ICredential> {
  return unwrap(await apiClient.PUT("/api/v1/providers/{providerId}/credentials", { params: { path: { providerId } }, body }));
}

export async function deleteCredential(providerId: string, purpose: ICredentialPurpose, scope: string): Promise<void> {
  ensureOk(
    await apiClient.DELETE("/api/v1/providers/{providerId}/credentials", {
      params: { path: { providerId }, query: { purpose, scope } },
    }),
  );
}
