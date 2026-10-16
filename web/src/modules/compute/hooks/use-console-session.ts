import { useMutation } from "@tanstack/react-query";

import { createConsoleSession, type IConsoleKind } from "../services/compute-service";

/** Issues a console token; called again to reconnect. */
export function useConsoleSession(providerId: string, externalId: string) {
  return useMutation({ mutationFn: (kind: IConsoleKind) => createConsoleSession(providerId, externalId, kind) });
}
