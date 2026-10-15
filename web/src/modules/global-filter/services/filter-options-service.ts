import { ApiError, apiClient, isProblem } from "@/shared/api-client";

export interface IFilterOption {
  id: string;
  name: string;
}

export async function fetchSiteOptions(): Promise<IFilterOption[]> {
  const { data, error, response } = await apiClient.GET("/api/v1/sites");
  if (data) return data.items.map(({ id, name }) => ({ id, name }));
  throw new ApiError(response.status, isProblem(error) ? error : undefined);
}

export async function fetchProjectOptions(): Promise<IFilterOption[]> {
  const { data, error, response } = await apiClient.GET("/api/v1/projects");
  if (data) return data.items.map(({ id, name }) => ({ id, name }));
  throw new ApiError(response.status, isProblem(error) ? error : undefined);
}
