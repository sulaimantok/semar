import { X } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router";

import { useGlobalFilter, useProjectOptions, useSiteOptions } from "@/modules/global-filter";
import { cn } from "@/shared/utils/cn";
import { formatAge, formatMemoryMiB } from "@/shared/utils/format";

import { useProviders } from "../../hooks/use-providers";
import { useVmPages } from "../../hooks/use-vm-pages";
import type { IArch, IResource } from "../../services/compute-service";
import { vmPath } from "../../utils/vm-path";

import { vmListStyles as styles } from "./vm-list.styles";

const statusDot: Record<string, string> = {
  running: styles.dotRunning,
  stopped: styles.dotStopped,
  paused: styles.dotWarn,
  building: styles.dotWarn,
  error: styles.dotError,
};

function num(v: unknown): number | undefined {
  return typeof v === "number" ? v : undefined;
}

function ips(v: unknown): string {
  return Array.isArray(v) ? v.filter((x) => typeof x === "string").join(", ") : "";
}

const ARCHES: readonly IArch[] = ["amd64", "arm64", "unknown"];

export function VmListPage() {
  const [providerId, setProviderId] = useState<string>("");
  const [arch, setArch] = useState<IArch | "">("");
  const global = useGlobalFilter();
  const providers = useProviders();
  const sites = useSiteOptions();
  const projects = useProjectOptions();
  const vms = useVmPages({
    providerId: providerId || undefined,
    arch: arch || undefined,
    siteId: global.siteId,
    projectId: global.projectId,
  });

  const lookups = useMemo(() => {
    const provider = new Map((providers.data ?? []).map((p) => [p.id, p]));
    const site = new Map((sites.data ?? []).map((s) => [s.id, s.name]));
    const project = new Map((projects.data ?? []).map((p) => [p.id, p.name]));
    return { provider, site, project };
  }, [providers.data, sites.data, projects.data]);

  const chips: { key: string; label: string; clear: () => void }[] = [];
  if (global.siteId) {
    chips.push({ key: "site", label: `Site: ${lookups.site.get(global.siteId) ?? global.siteId}`, clear: () => global.set({ siteId: undefined }) });
  }
  if (global.projectId) {
    chips.push({
      key: "project",
      label: `Project: ${lookups.project.get(global.projectId) ?? global.projectId}`,
      clear: () => global.set({ projectId: undefined }),
    });
  }
  if (providerId) {
    chips.push({
      key: "provider",
      label: `Provider: ${lookups.provider.get(providerId)?.name ?? providerId}`,
      clear: () => setProviderId(""),
    });
  }
  if (arch) chips.push({ key: "arch", label: `Arch: ${arch}`, clear: () => setArch("") });

  const rows: IResource[] = vms.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.titleGroup}>
          <h1 className={styles.title}>Virtual machines</h1>
          <p className={styles.subtitle}>From the inventory cache, across all providers you can see.</p>
        </div>
        <div className={styles.filters}>
          <label className={styles.filterLabel} htmlFor="vm-provider">
            Provider
          </label>
          <select
            id="vm-provider"
            className={styles.select}
            value={providerId}
            onChange={(e) => setProviderId(e.target.value)}
          >
            <option value="">All providers</option>
            {(providers.data ?? []).map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          <label className={styles.filterLabel} htmlFor="vm-arch">
            Arch
          </label>
          <select id="vm-arch" className={styles.select} value={arch} onChange={(e) => setArch(e.target.value as IArch | "")}>
            <option value="">All</option>
            {ARCHES.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
        </div>
      </header>

      {chips.length > 0 && (
        <ul className={styles.chips} aria-label="Active filters">
          {chips.map((c) => (
            <li key={c.key}>
              <button type="button" className={styles.chip} onClick={c.clear} aria-label={`Remove filter ${c.label}`}>
                {c.label}
                <X className={styles.chipIcon} aria-hidden="true" />
              </button>
            </li>
          ))}
        </ul>
      )}

      {vms.isError && (
        <p className={styles.error} role="alert">
          Could not load virtual machines.
        </p>
      )}

      {vms.isPending ? (
        <p className={styles.muted}>Loading…</p>
      ) : rows.length === 0 ? (
        <p className={styles.empty}>
          {chips.length > 0
            ? "No virtual machines match the active filters."
            : "No virtual machines yet. They appear after a provider is registered and its first sync completes."}
        </p>
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th className={styles.th}>Name</th>
                <th className={styles.th}>Status</th>
                <th className={styles.th}>Provider</th>
                <th className={styles.th}>Site</th>
                <th className={styles.th}>Project</th>
                <th className={styles.th}>Arch</th>
                <th className={styles.th}>vCPU</th>
                <th className={styles.th}>Memory</th>
                <th className={styles.th}>IP addresses</th>
                <th className={styles.th}>Updated</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((vm) => {
                const vcpus = num(vm.spec.vcpus);
                const mem = num(vm.spec.memoryMiB);
                const provider = lookups.provider.get(vm.providerId);
                const siteId = provider?.siteId;
                return (
                  <tr key={`${vm.providerId}/${vm.externalId}`}>
                    <td className={cn(styles.td, styles.name)}>
                      <Link className={styles.link} to={vmPath(vm)}>
                        {vm.name || vm.externalId}
                      </Link>
                      {vm.orphanedAt && <span className={styles.orphanBadge}>orphaned</span>}
                    </td>
                    <td className={styles.td}>
                      <span className={styles.status}>
                        <span className={cn(styles.dot, statusDot[vm.status] ?? styles.dotStopped)} aria-hidden="true" />
                        {vm.status || "unknown"}
                      </span>
                    </td>
                    <td className={styles.td}>{provider?.name ?? vm.providerId}</td>
                    <td className={styles.td}>{siteId ? (lookups.site.get(siteId) ?? siteId) : "—"}</td>
                    <td className={styles.td}>
                      {vm.projectId ? (
                        (lookups.project.get(vm.projectId) ?? vm.projectId)
                      ) : (
                        <span className={styles.badge}>unassigned</span>
                      )}
                    </td>
                    <td className={cn(styles.td, styles.mono)}>{vm.arch}</td>
                    <td className={styles.td}>{vcpus ?? "—"}</td>
                    <td className={styles.td}>{mem !== undefined ? formatMemoryMiB(mem) : "—"}</td>
                    <td className={cn(styles.td, styles.mono)}>{ips(vm.spec.ipAddresses) || "—"}</td>
                    <td className={cn(styles.td, styles.muted)} title={vm.observedAt}>
                      {formatAge(vm.observedAt)}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {rows.length > 0 && (
        <div className={styles.footer}>
          <span>{rows.length} shown</span>
          {vms.hasNextPage && (
            <button
              type="button"
              className={styles.more}
              disabled={vms.isFetchingNextPage}
              onClick={() => void vms.fetchNextPage()}
            >
              {vms.isFetchingNextPage ? "Loading…" : "Load more"}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
