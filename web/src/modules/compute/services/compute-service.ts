import { ApiError, apiClient, isProblem, type components } from "@/shared/api-client";

export type IResource = components["schemas"]["Resource"];
export type IResourceList = components["schemas"]["ResourceList"];
export type IProvider = components["schemas"]["Provider"];
export type ICapacity = components["schemas"]["Capacity"];
export type ICountByKey = components["schemas"]["CountByKey"];

export interface ICapacityQuery {
  providerId?: string;
  siteId?: string;
  projectId?: string;
}

export type IResourceDetail = components["schemas"]["ResourceDetail"];
export type IResourceChange = components["schemas"]["ResourceChange"];
export type IArch = components["schemas"]["Resource"]["arch"];

/** Narrowing applied to the VM list; the API applies RBAC on top. */
export interface IVmFilter {
  providerId?: string;
  siteId?: string;
  projectId?: string;
  arch?: IArch;
}

export interface IVmQuery extends IVmFilter {
  cursor?: string;
}

export const VM_PAGE_SIZE = 50;

export async function fetchVms({ providerId, siteId, projectId, arch, cursor }: IVmQuery): Promise<IResourceList> {
  const { data, error, response } = await apiClient.GET("/api/v1/resources", {
    params: { query: { kind: "vm", providerId, siteId, projectId, arch, cursor, limit: VM_PAGE_SIZE } },
  });
  if (data) return data;
  throw new ApiError(response.status, isProblem(error) ? error : undefined);
}

export async function fetchProviders(): Promise<IProvider[]> {
  const { data, error, response } = await apiClient.GET("/api/v1/providers");
  if (data) return data.items;
  throw new ApiError(response.status, isProblem(error) ? error : undefined);
}

export async function fetchCapacity({ providerId, siteId, projectId }: ICapacityQuery): Promise<ICapacity> {
  const { data, error, response } = await apiClient.GET("/api/v1/capacity", {
    params: { query: { providerId, siteId, projectId } },
  });
  if (data) return data;
  throw new ApiError(response.status, isProblem(error) ? error : undefined);
}

/** One VM with its recent history, or null when it is missing or not visible. */
export async function fetchVm(providerId: string, externalId: string): Promise<IResourceDetail | null> {
  const { data, error, response } = await apiClient.GET("/api/v1/resources/{providerId}/{kind}/{externalId}", {
    params: { path: { providerId, kind: "vm", externalId } },
  });
  if (data) return data;
  if (response.status === 404) return null;
  throw new ApiError(response.status, isProblem(error) ? error : undefined);
}
