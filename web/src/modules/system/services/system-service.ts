import { ApiError, apiClient, isProblem, type components } from "@/shared/api-client";

export type IVersionInfo = components["schemas"]["VersionInfo"];

export interface IReadiness {
  ready: boolean;
  /** Names of failing checks, as reported by /readyz. */
  detail?: string;
}

export async function fetchVersion(): Promise<IVersionInfo> {
  const { data, error, response } = await apiClient.GET("/api/v1/version");
  if (data) return data;
  throw new ApiError(response.status, isProblem(error) ? error : undefined);
}

/** A 503 from /readyz is an expected answer ("not ready"), not an error. */
export async function fetchReadiness(): Promise<IReadiness> {
  const { data, error, response } = await apiClient.GET("/readyz");
  if (data) return { ready: true };
  if (response.status === 503 && isProblem(error)) {
    return { ready: false, detail: error.detail };
  }
  throw new ApiError(response.status, isProblem(error) ? error : undefined);
}
