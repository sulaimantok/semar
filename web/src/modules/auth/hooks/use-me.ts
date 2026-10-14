import { useQuery } from "@tanstack/react-query";

import { fetchMe } from "../services/auth-service";

import { authKeys } from "./auth-keys";

export function useMe() {
  return useQuery({ queryKey: authKeys.me(), queryFn: fetchMe, staleTime: 60_000, retry: false });
}
