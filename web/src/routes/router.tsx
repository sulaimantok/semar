import { createBrowserRouter } from "react-router";

import { NotFoundPage, OverviewPage } from "@/modules/system";
import { AppShell } from "@/shared/components/app-shell";

// Thin route table: mounts module pages inside the shell; no logic here.
export const router = createBrowserRouter([
  {
    path: "/",
    element: <AppShell />,
    children: [
      { index: true, element: <OverviewPage /> },
      { path: "*", element: <NotFoundPage /> },
    ],
  },
]);
