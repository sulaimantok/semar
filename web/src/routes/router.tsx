import { Suspense } from "react";
import { createBrowserRouter, Navigate } from "react-router";

import { LoginPage, RequireAuth, UserMenu } from "@/modules/auth";
import { VmConsolePage, VmDetailPage, VmListPage } from "@/modules/compute";
import { GlobalFilterBar } from "@/modules/global-filter";
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
        <AppShell header={<GlobalFilterBar />} footer={<UserMenu />} />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <OverviewPage /> },
      { path: "compute", element: <VmListPage /> },
      { path: "compute/vms/:providerId/:externalId", element: <VmDetailPage /> },
      {
        path: "compute/vms/:providerId/:externalId/console/:kind",
        element: (
          <Suspense fallback={<p>Loading console…</p>}>
            <VmConsolePage />
          </Suspense>
        ),
      },
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
