import type { ReactNode } from "react";
import { NavLink, Outlet } from "react-router";

import { cn } from "@/shared/utils/cn";

import { appShellStyles as styles } from "./app-shell.styles";
import { visibleNavItems } from "./nav-items";

export interface IAppShellProps {
  /** Rendered at the bottom of the sidebar (e.g. the signed-in user menu). */
  footer?: ReactNode;
  /** Rendered above the page content (e.g. the global filter). */
  header?: ReactNode;
}

export function AppShell({ footer, header }: IAppShellProps) {
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
        {footer && <div className={styles.footer}>{footer}</div>}
      </aside>
      <div className={styles.content}>
        {header}
        <main className={styles.main}>
          <Outlet />
        </main>
      </div>
    </div>
  );
}
