import type { IMe } from "../services/auth-service";

/**
 * Mirrors the server's notion of a global admin: an admin binding scoped to
 * no project and no provider. Used only to hide controls the API would
 * refuse; the API remains the authority.
 */
export function isGlobalAdmin(me: IMe | null | undefined): boolean {
  return !!me?.roles.some((r) => r.role === "admin" && !r.projectId && !r.providerId);
}
