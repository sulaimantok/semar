import type { IMe } from "../services/auth-service";

/**
 * Mirrors the server's notion of a global admin: an admin binding scoped to
 * no project and no provider. Used only to hide controls the API would
 * refuse; the API remains the authority.
 */
export function isGlobalAdmin(me: IMe | null | undefined): boolean {
  return !!me?.roles.some((r) => r.role === "admin" && !r.projectId && !r.providerId);
}

/**
 * Whether me holds an Admin or Operator binding covering a resource of
 * projectId on providerId, as the API requires for consoles. Hides
 * controls only; the API remains the authority.
 */
export function canOperate(me: IMe | null | undefined, target: { projectId?: string; providerId: string }): boolean {
  return !!me?.roles.some(
    (r) =>
      (r.role === "admin" || r.role === "operator") &&
      // A scoped binding never covers resources without a project.
      (r.projectId ? r.projectId === target.projectId : !r.providerId || !!target.projectId) &&
      (!r.providerId || r.providerId === target.providerId),
  );
}
