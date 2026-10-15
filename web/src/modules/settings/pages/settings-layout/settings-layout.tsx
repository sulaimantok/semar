import { NavLink, Outlet } from "react-router";

import { useIsAdmin } from "@/modules/auth";
import { cn } from "@/shared/utils/cn";

import { settingsLayoutStyles as styles } from "./settings-layout.styles";

const TABS = [
  { to: "providers", label: "Providers" },
  { to: "sites", label: "Sites" },
  { to: "projects", label: "Projects" },
] as const;

export function SettingsLayout() {
  const isAdmin = useIsAdmin();
  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <h1 className={styles.title}>Settings</h1>
        <p className={styles.subtitle}>Where resources live (sites, providers) and who owns them (projects).</p>
      </header>
      {!isAdmin && (
        <p className={styles.readOnly}>Read-only: only global admins can change settings or manage credentials.</p>
      )}
      <nav className={styles.tabs} aria-label="Settings">
        {TABS.map((t) => (
          <NavLink key={t.to} to={t.to} className={({ isActive }) => cn(styles.tab, isActive && styles.tabActive)}>
            {t.label}
          </NavLink>
        ))}
      </nav>
      <Outlet />
    </div>
  );
}
