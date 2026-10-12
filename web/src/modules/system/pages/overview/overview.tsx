import { cn } from "@/shared/utils/cn";

import { useReadiness } from "../../hooks/use-readiness";
import { useVersion } from "../../hooks/use-version";

import { overviewStyles as styles } from "./overview.styles";

export function OverviewPage() {
  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <h1 className={styles.title}>Overview</h1>
        <p className={styles.subtitle}>Health of the SEMAR control plane.</p>
      </header>

      <div className={styles.grid}>
        <ApiStatusCard />
        <BuildCard />
      </div>

      <p className={styles.empty}>
        Inventory from OpenStack and Proxmox appears here once providers are registered.
      </p>
    </div>
  );
}

function ApiStatusCard() {
  const { data, isPending, isError, dataUpdatedAt } = useReadiness();

  let label = "Checking…";
  let dot: string = styles.dotUnknown;
  let hint: string | undefined;
  if (isError) {
    label = "Unreachable";
    dot = styles.dotDanger;
    hint = "semar-api did not answer.";
  } else if (!isPending && data.ready) {
    label = "Ready";
    dot = styles.dotOk;
  } else if (!isPending) {
    label = "Not ready";
    dot = styles.dotWarn;
    hint = data.detail;
  }

  return (
    <section className={styles.card} aria-labelledby="api-status-title">
      <h2 id="api-status-title" className={styles.cardTitle}>
        API status
      </h2>
      <div className={styles.status} role="status">
        <span className={cn(styles.dot, dot)} aria-hidden="true" />
        {label}
      </div>
      {hint && <p className={styles.hint}>{hint}</p>}
      {dataUpdatedAt > 0 && (
        <p className={styles.hint}>Checked at {new Date(dataUpdatedAt).toLocaleTimeString()}</p>
      )}
    </section>
  );
}

function BuildCard() {
  const { data, isPending, isError } = useVersion();

  return (
    <section className={styles.card} aria-labelledby="build-title">
      <h2 id="build-title" className={styles.cardTitle}>
        Build
      </h2>
      {isPending && <p className={styles.hint}>Loading…</p>}
      {isError && <p className={styles.hint}>Build information unavailable.</p>}
      {data && (
        <dl className={styles.facts}>
          <dt className={styles.factLabel}>Version</dt>
          <dd className={styles.factValue}>{data.version}</dd>
          <dt className={styles.factLabel}>Commit</dt>
          <dd className={styles.factValue}>{data.commit}</dd>
          <dt className={styles.factLabel}>Platform</dt>
          <dd className={styles.factValue}>{data.platform}</dd>
          <dt className={styles.factLabel}>Go</dt>
          <dd className={styles.factValue}>{data.goVersion}</dd>
        </dl>
      )}
    </section>
  );
}
