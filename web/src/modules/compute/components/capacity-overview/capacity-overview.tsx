import { formatMemoryMiB } from "@/shared/utils/format";

import { useCapacity } from "../../hooks/use-capacity";
import type { ICountByKey } from "../../services/compute-service";

import { capacityOverviewStyles as styles } from "./capacity-overview.styles";

function percent(used: number, total: number): number {
  return total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0;
}

function Breakdown({ label, counts }: { label: string; counts: ICountByKey[] }) {
  if (counts.length === 0) return null;
  return (
    <ul className={styles.breakdown} aria-label={label}>
      {counts.map((c) => (
        <li key={c.key} className={styles.chip}>
          {c.key} · {c.count}
        </li>
      ))}
    </ul>
  );
}

function Meter({ label, used, total, format }: { label: string; used: number; total: number; format: (n: number) => string }) {
  const pct = percent(used, total);
  return (
    <div className={styles.meterRow}>
      <div className={styles.meterLabel}>
        <span>{label}</span>
        <span className={styles.hint}>
          {format(used)} / {format(total)} ({pct}%)
        </span>
      </div>
      <progress className={styles.meter} aria-label={`${label} used`} value={pct} max={100} />
    </div>
  );
}

/**
 * Capacity Overview: totals over the VMs the signed-in user may see. The API
 * applies the same scoping as the VM list, so a project viewer only ever
 * sees their projects; physical capacity is shown to global roles only.
 */
export function CapacityOverview() {
  const { data, isPending, isError } = useCapacity();

  return (
    <section className={styles.section} aria-labelledby="capacity-title">
      <header className={styles.heading}>
        <h2 id="capacity-title" className={styles.title}>
          Capacity
        </h2>
        <p className={styles.subtitle}>Across the providers and projects you can see.</p>
      </header>

      {isError && (
        <p className={styles.error} role="alert">
          Could not load capacity.
        </p>
      )}
      {isPending && <p className={styles.hint}>Loading…</p>}

      {data && data.vms.total === 0 && !data.hypervisors?.total && (
        <p className={styles.empty}>
          No inventory yet. Capacity appears after a provider is registered and its first sync completes.
        </p>
      )}

      {data && (data.vms.total > 0 || !!data.hypervisors?.total) && (
        <div className={styles.grid}>
          <article className={styles.card} aria-labelledby="capacity-vms">
            <h3 id="capacity-vms" className={styles.cardTitle}>
              Virtual machines
            </h3>
            <p className={styles.stat}>{data.vms.total}</p>
            <Breakdown label="VMs by status" counts={data.vms.byStatus} />
            <Breakdown label="VMs by architecture" counts={data.vms.byArch} />
          </article>

          <article className={styles.card} aria-labelledby="capacity-allocated">
            <h3 id="capacity-allocated" className={styles.cardTitle}>
              Allocated to VMs
            </h3>
            <p className={styles.stat}>
              {data.vms.vcpus}
              <span className={styles.statUnit}>vCPU</span>
            </p>
            <p className={styles.hint}>{formatMemoryMiB(data.vms.memoryMiB)} memory</p>
          </article>

          {data.hypervisors && (
            <article className={styles.card} aria-labelledby="capacity-physical">
              <h3 id="capacity-physical" className={styles.cardTitle}>
                Hypervisors
              </h3>
              <p className={styles.stat}>{data.hypervisors.total}</p>
              <Meter label="vCPU" used={data.hypervisors.vcpusUsed} total={data.hypervisors.vcpus} format={String} />
              <Meter
                label="Memory"
                used={data.hypervisors.memoryMiBUsed}
                total={data.hypervisors.memoryMiB}
                format={formatMemoryMiB}
              />
              <Breakdown label="Hypervisors by architecture" counts={data.hypervisors.byArch} />
            </article>
          )}
        </div>
      )}
    </section>
  );
}
