import { isGlobalAdmin } from "../utils/roles";

import { useMe } from "./use-me";

/** Whether the signed-in user is a global admin (see isGlobalAdmin). */
export function useIsAdmin(): boolean {
  const { data } = useMe();
  return isGlobalAdmin(data);
}
