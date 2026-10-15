import { ApiError, apiClient, isProblem, type components } from "@/shared/api-client";

export type IResource = components["schemas"]["Resource"];
export type IResourceList = components["schemas"]["ResourceList"];
export type IProvider = components["schemas"]["Provider"];
export type ICapacity = components["schemas"]["Capacity"];
export type ICountByKey = components["schemas"]["CountByKey"];

export interface ICapacityQuery {
  providerId?: string;
  projectId?: string;
}

export interface IVmQuery {
  providerId?: string;
  cursor?: string;
}

export const VM_PAGE_SIZE = 50;

export async function fetchVms({ providerId, cursor }: IVmQuery): Promise<IResourceList> {
  const { data, error, response } = await apiClient.GET("/api/v1/resources", {
    params: { query: { kind: "vm", providerId, cursor, limit: VM_PAGE_SIZE } },
  });
  if (data) return data;
  throw new ApiError(response.status, isProblem(error) ? error : undefined);
}

export async function fetchProviders(): Promise<IProvider[]> {
  const { data, error, response } = await apiClient.GET("/api/v1/providers");
  if (data) return data.items;
  throw new ApiError(response.status, isProblem(error) ? error : undefined);
}

export async function fetchCapacity({ providerId, projectId }: ICapacityQuery): Promise<ICapacity> {
  const { data, error, response } = await apiClient.GET("/api/v1/capacity", {
    params: { query: { providerId, projectId } },
  });
  if (data) return data;
  throw new ApiError(response.status, isProblem(error) ? error : undefined);
}
