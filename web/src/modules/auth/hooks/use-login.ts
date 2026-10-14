import { useMutation, useQueryClient } from "@tanstack/react-query";

import { login } from "../services/auth-service";

import { authKeys } from "./auth-keys";

export function useLogin() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ username, password }: { username: string; password: string }) => login(username, password),
    onSuccess: (me) => {
      queryClient.setQueryData(authKeys.me(), me);
    },
  });
}
