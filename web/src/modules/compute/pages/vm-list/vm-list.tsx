import { useMemo, useState } from "react";

import { cn } from "@/shared/utils/cn";
import { formatAge, formatMemoryMiB } from "@/shared/utils/format";

import { useProviders } from "../../hooks/use-providers";
import { useVmPages } from "../../hooks/use-vm-pages";
import type { IResource } from "../../services/compute-service";

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

export function VmListPage() {
  const [providerId, setProviderId] = useState<string>("");
  const providers = useProviders();
  const vms = useVmPages(providerId || undefined);

  const providerName = useMemo(() => {
    const m = new Map<string, string>();
    for (const p of providers.data ?? []) m.set(p.id, p.name);
    return m;
  }, [providers.data]);

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
        </div>
      </header>

      {vms.isError && (
        <p className={styles.error} role="alert">
          Could not load virtual machines.
        </p>
      )}

      {vms.isPending ? (
        <p className={styles.muted}>Loading…</p>
      ) : rows.length === 0 ? (
        <p className={styles.empty}>
          No virtual machines yet. They appear after a provider is registered and its first sync completes.
        </p>
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th className={styles.th}>Name</th>
                <th className={styles.th}>Status</th>
                <th className={styles.th}>Provider</th>
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
                return (
                  <tr key={`${vm.providerId}/${vm.externalId}`}>
                    <td className={cn(styles.td, styles.name)}>{vm.name || vm.externalId}</td>
                    <td className={styles.td}>
                      <span className={styles.status}>
                        <span className={cn(styles.dot, statusDot[vm.status] ?? styles.dotStopped)} aria-hidden="true" />
                        {vm.status || "unknown"}
                      </span>
                    </td>
                    <td className={styles.td}>{providerName.get(vm.providerId) ?? vm.providerId}</td>
                    <td className={styles.td}>
                      {vm.projectId ?? <span className={styles.badge}>unassigned</span>}
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
