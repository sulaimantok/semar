import { Link, useParams } from "react-router";

import { useProjectOptions, useSiteOptions } from "@/modules/global-filter";
import { cn } from "@/shared/utils/cn";
import { formatAge, formatMemoryMiB } from "@/shared/utils/format";

import { useProviders } from "../../hooks/use-providers";
import { useVm } from "../../hooks/use-vm";

import { vmDetailStyles as styles } from "./vm-detail.styles";

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

function strings(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
}

function text(v: unknown): string | undefined {
  return typeof v === "string" && v ? v : undefined;
}

function tags(v: unknown): [string, string][] {
  if (Array.isArray(v)) return strings(v).map((t) => [t, ""]);
  if (typeof v === "object" && v !== null) {
    return Object.entries(v).filter((e): e is [string, string] => typeof e[1] === "string");
  }
  return [];
}

export function VmDetailPage() {
  const { providerId = "", externalId = "" } = useParams();
  const vm = useVm(providerId, externalId);
  const providers = useProviders();
  const sites = useSiteOptions();
  const projects = useProjectOptions();

  const back = (
    <Link className={styles.back} to="/compute">
      ← Virtual machines
    </Link>
  );

  if (vm.isPending) return <p className={styles.muted}>Loading…</p>;
  if (vm.isError) {
    return (
      <div className={styles.page}>
        {back}
        <p className={styles.error} role="alert">
          Could not load this virtual machine.
        </p>
      </div>
    );
  }
  if (vm.data === null) {
    return (
      <div className={styles.page}>
        {back}
        <p className={styles.empty}>This virtual machine does not exist or is outside the projects you can see.</p>
      </div>
    );
  }

  const { resource: r, changes } = vm.data;
  const provider = providers.data?.find((p) => p.id === r.providerId);
  const siteName = provider?.siteId ? (sites.data?.find((s) => s.id === provider.siteId)?.name ?? provider.siteId) : undefined;
  const projectName = r.projectId ? (projects.data?.find((p) => p.id === r.projectId)?.name ?? r.projectId) : undefined;
  const vcpus = num(r.spec.vcpus);
  const memory = num(r.spec.memoryMiB);
  const ips = strings(r.spec.ipAddresses);
  const vmTags = tags(r.spec.tags);

  return (
    <div className={styles.page}>
      {back}
      <header className={styles.header}>
        <div className={styles.titleGroup}>
          <h1 className={styles.title}>{r.name || r.externalId}</h1>
          <p className={styles.subtitle}>{r.externalId}</p>
        </div>
        <span className={styles.status} role="status">
          <span className={cn(styles.dot, statusDot[r.status] ?? styles.dotStopped)} aria-hidden="true" />
          {r.status || "unknown"}
        </span>
      </header>

      {r.orphanedAt && (
        <p className={styles.orphaned} role="alert">
          Not reported by the provider since {new Date(r.orphanedAt).toLocaleString()} — it was probably deleted
          outside SEMAR. Kept here for its history.
        </p>
      )}

      <div className={styles.grid}>
        <section className={styles.card} aria-labelledby="vm-where">
          <h2 id="vm-where" className={styles.cardTitle}>
            Location &amp; ownership
          </h2>
          <dl className={styles.facts}>
            <dt className={styles.factLabel}>Provider</dt>
            <dd className={styles.factValue}>{provider?.name ?? r.providerId}</dd>
            <dt className={styles.factLabel}>Site</dt>
            <dd className={styles.factValue}>{siteName ?? "—"}</dd>
            <dt className={styles.factLabel}>Project</dt>
            <dd className={styles.factValue}>{projectName ?? <span className={styles.badge}>unassigned</span>}</dd>
            <dt className={styles.factLabel}>External scope</dt>
            <dd className={cn(styles.factValue, styles.mono)}>{r.externalScope || "—"}</dd>
            <dt className={styles.factLabel}>Hypervisor</dt>
            <dd className={cn(styles.factValue, styles.mono)}>{text(r.spec.hypervisorId) ?? "—"}</dd>
          </dl>
        </section>

        <section className={styles.card} aria-labelledby="vm-spec">
          <h2 id="vm-spec" className={styles.cardTitle}>
            Specification
          </h2>
          <dl className={styles.facts}>
            <dt className={styles.factLabel}>Arch</dt>
            <dd className={cn(styles.factValue, styles.mono)}>{r.arch}</dd>
            <dt className={styles.factLabel}>vCPU</dt>
            <dd className={styles.factValue}>{vcpus ?? "—"}</dd>
            <dt className={styles.factLabel}>Memory</dt>
            <dd className={styles.factValue}>{memory !== undefined ? formatMemoryMiB(memory) : "—"}</dd>
            <dt className={styles.factLabel}>Image</dt>
            <dd className={cn(styles.factValue, styles.mono)}>{text(r.spec.imageId) ?? "—"}</dd>
            <dt className={styles.factLabel}>IP addresses</dt>
            <dd className={cn(styles.factValue, styles.mono)}>{ips.length ? ips.join(", ") : "—"}</dd>
            <dt className={styles.factLabel}>Tags</dt>
            <dd className={styles.factValue}>
              {vmTags.length ? (
                <span className={styles.tags}>
                  {vmTags.map(([k, v]) => (
                    <span key={k} className={styles.badge}>
                      {v ? `${k}=${v}` : k}
                    </span>
                  ))}
                </span>
              ) : (
                "—"
              )}
            </dd>
          </dl>
        </section>

        <section className={styles.card} aria-labelledby="vm-freshness">
          <h2 id="vm-freshness" className={styles.cardTitle}>
            Data age
          </h2>
          <dl className={styles.facts}>
            <dt className={styles.factLabel}>Last observed</dt>
            <dd className={styles.factValue} title={r.observedAt}>
              {formatAge(r.observedAt)}
            </dd>
            <dt className={styles.factLabel}>First seen</dt>
            <dd className={styles.factValue} title={r.firstSeenAt}>
              {new Date(r.firstSeenAt).toLocaleString()}
            </dd>
          </dl>
        </section>

        <section className={styles.card} aria-labelledby="vm-history">
          <h2 id="vm-history" className={styles.cardTitle}>
            History
          </h2>
          {changes.length === 0 ? (
            <p className={styles.muted}>No changes recorded.</p>
          ) : (
            <ol className={styles.history}>
              {changes.map((c) => (
                <li key={c.id} className={styles.historyItem}>
                  <span className={styles.change}>{c.change}</span>
                  <span className={styles.muted} title={c.observedAt}>
                    {new Date(c.observedAt).toLocaleString()}
                    {c.projectId ? ` · ${projects.data?.find((p) => p.id === c.projectId)?.name ?? c.projectId}` : ""}
                  </span>
                </li>
              ))}
            </ol>
          )}
        </section>
      </div>
    </div>
  );
}
