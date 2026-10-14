import { useMutation, useQueryClient } from "@tanstack/react-query";

import { logout } from "../services/auth-service";

import { authKeys } from "./auth-keys";

export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: logout,
    onSettled: () => {
      // Drop every cached response: it belonged to the previous user.
      queryClient.clear();
      queryClient.setQueryData(authKeys.me(), null);
    },
  });
}
