import { createBrowserRouter, Navigate } from "react-router";

import { LoginPage, RequireAuth, UserMenu } from "@/modules/auth";
import { VmListPage } from "@/modules/compute";
import { ProjectsPage, ProviderDetailPage, ProvidersPage, SettingsLayout, SitesPage } from "@/modules/settings";
import { NotFoundPage, OverviewPage } from "@/modules/system";
import { AppShell } from "@/shared/components/app-shell";

// Thin route table: mounts module pages inside the shell; no logic here.
export const router = createBrowserRouter([
  { path: "/login", element: <LoginPage /> },
  {
    path: "/",
    element: (
      <RequireAuth>
        <AppShell footer={<UserMenu />} />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <OverviewPage /> },
      { path: "compute", element: <VmListPage /> },
      {
        path: "settings",
        element: <SettingsLayout />,
        children: [
          { index: true, element: <Navigate to="providers" replace /> },
          { path: "providers", element: <ProvidersPage /> },
          { path: "providers/:providerId", element: <ProviderDetailPage /> },
          { path: "sites", element: <SitesPage /> },
          { path: "projects", element: <ProjectsPage /> },
        ],
      },
      { path: "*", element: <NotFoundPage /> },
    ],
  },
]);
