import { useEffect } from "react";

import { cn } from "@/shared/utils/cn";

import { useProjectOptions, useSiteOptions } from "../../hooks/use-filter-options";
import { useGlobalFilter } from "../../hooks/use-global-filter";

import { globalFilterBarStyles as styles } from "./global-filter-bar.styles";

/**
 * Site and Project filter applied across inventory views. Defaults to All;
 * an active filter is always visible and can be reset in one click.
 */
export function GlobalFilterBar() {
  const { siteId, projectId, isActive, set, reset } = useGlobalFilter();
  const sites = useSiteOptions();
  const projects = useProjectOptions();

  // Forget a stored choice that no longer exists (deleted, or not visible).
  useEffect(() => {
    if (siteId && sites.data && !sites.data.some((s) => s.id === siteId)) set({ siteId: undefined });
  }, [siteId, sites.data, set]);
  useEffect(() => {
    if (projectId && projects.data && !projects.data.some((p) => p.id === projectId)) set({ projectId: undefined });
  }, [projectId, projects.data, set]);

  return (
    <div className={styles.root} role="search" aria-label="Global filter">
      <label className={styles.label} htmlFor="global-site">
        Site
      </label>
      <select
        id="global-site"
        className={cn(styles.select, siteId && styles.selectActive)}
        value={siteId ?? ""}
        onChange={(e) => set({ siteId: e.target.value || undefined })}
      >
        <option value="">All sites</option>
        {(sites.data ?? []).map((s) => (
          <option key={s.id} value={s.id}>
            {s.name}
          </option>
        ))}
      </select>
      <label className={styles.label} htmlFor="global-project">
        Project
      </label>
      <select
        id="global-project"
        className={cn(styles.select, projectId && styles.selectActive)}
        value={projectId ?? ""}
        onChange={(e) => set({ projectId: e.target.value || undefined })}
      >
        <option value="">All projects</option>
        {(projects.data ?? []).map((p) => (
          <option key={p.id} value={p.id}>
            {p.name}
          </option>
        ))}
      </select>
      {isActive && (
        <>
          <span className={styles.active} role="status">
            <span className={styles.dot} aria-hidden="true" />
            Filter active
          </span>
          <button type="button" className={styles.reset} onClick={reset}>
            Reset
          </button>
        </>
      )}
    </div>
  );
}
