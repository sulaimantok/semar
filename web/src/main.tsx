import { QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "react-router";

import { authKeys } from "@/modules/auth";
import { router } from "@/routes/router";
import { ApiError } from "@/shared/api-client";

import "./index.css";

const queryClient: QueryClient = new QueryClient({
  // An expired session surfaces as 401 on any query: forget the user so
  // RequireAuth sends them back to /login.
  queryCache: new QueryCache({
    onError: (err) => {
      if (err instanceof ApiError && err.status === 401) queryClient.setQueryData(authKeys.me(), null);
    },
  }),
  defaultOptions: {
    queries: {
      retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 1,
      refetchOnWindowFocus: false,
    },
  },
});

const root = document.getElementById("root");
if (!root) throw new Error("#root element missing");

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
