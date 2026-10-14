import { ApiError, apiClient, isProblem, type components } from "@/shared/api-client";

export type IMe = components["schemas"]["Me"];

/** The signed-in user, or null when there is no valid session. */
export async function fetchMe(): Promise<IMe | null> {
  const { data, error, response } = await apiClient.GET("/api/v1/auth/me");
  if (data) return data;
  if (response.status === 401) return null;
  throw new ApiError(response.status, isProblem(error) ? error : undefined);
}

export async function login(username: string, password: string): Promise<IMe> {
  const { data, error, response } = await apiClient.POST("/api/v1/auth/login", { body: { username, password } });
  if (data) return data;
  throw new ApiError(response.status, isProblem(error) ? error : undefined);
}

export async function logout(): Promise<void> {
  const { error, response } = await apiClient.POST("/api/v1/auth/logout");
  if (!response.ok) throw new ApiError(response.status, isProblem(error) ? error : undefined);
}
