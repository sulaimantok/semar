import { NavLink, Outlet } from "react-router";

import { cn } from "@/shared/utils/cn";

import { appShellStyles as styles } from "./app-shell.styles";
import { visibleNavItems } from "./nav-items";

export function AppShell() {
  return (
    <div className={styles.root}>
      <aside className={styles.sidebar}>
        <div className={styles.brand}>
          <span className={styles.brandMark} aria-hidden="true">
            S
          </span>
          <div>
            <div className={styles.brandName}>SEMAR</div>
            <div className={styles.brandTag}>Unified cloud control plane</div>
          </div>
        </div>
        <nav className={styles.nav} aria-label="Main">
          {visibleNavItems().map(({ label, to, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              end={to === "/"}
              className={({ isActive }) => cn(styles.navLink, isActive && styles.navLinkActive)}
            >
              <Icon className={styles.navIcon} aria-hidden="true" />
              {label}
            </NavLink>
          ))}
        </nav>
      </aside>
      <main className={styles.main}>
        <Outlet />
      </main>
    </div>
  );
}
