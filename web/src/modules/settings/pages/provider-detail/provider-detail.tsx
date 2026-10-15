import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router";

import { useIsAdmin } from "@/modules/auth";
import { cn } from "@/shared/utils/cn";
import { formatAge } from "@/shared/utils/format";

import { ApiErrorAlert } from "../../components/api-error-alert";
import { useDeleteProvider } from "../../hooks/use-settings-mutations";
import { useProvider, useSites, useSyncStatus } from "../../hooks/use-settings-queries";

import { ConnectionCard } from "./components/connection-card";
import { CredentialsSection } from "./components/credentials-section";
import { EditProviderForm } from "./components/edit-provider-form";
import { providerDetailStyles as styles } from "./provider-detail.styles";

const healthDot: Record<string, string> = {
  healthy: styles.dotHealthy,
  degraded: styles.dotDegraded,
  unreachable: styles.dotUnreachable,
};

export function ProviderDetailPage() {
  const { providerId = "" } = useParams();
  const isAdmin = useIsAdmin();
  const [editing, setEditing] = useState(false);
  const provider = useProvider(providerId);
  const sites = useSites();

  if (provider.isPending) return <p className={styles.muted}>Loading…</p>;
  if (provider.isError) {
    return (
      <div className={styles.section}>
        <Link className={styles.back} to="/settings/providers">
          ← Providers
        </Link>
        <ApiErrorAlert error={provider.error} />
      </div>
    );
  }
  const p = provider.data;
  const site = p.siteId ? (sites.data?.find((s) => s.id === p.siteId)?.name ?? p.siteId) : undefined;

  return (
    <div className={styles.section}>
      <Link className={styles.back} to="/settings/providers">
        ← Providers
      </Link>
      <div className={styles.titleRow}>
        <h2 className={styles.title}>{p.name}</h2>
        {isAdmin && (
          <div className={styles.titleActions}>
            {!editing && (
              <button type="button" className={styles.secondary} onClick={() => setEditing(true)}>
                Edit
              </button>
            )}
            <DeleteProvider providerId={p.id} name={p.name} />
          </div>
        )}
      </div>

      {isAdmin && editing && <EditProviderForm provider={p} onDone={() => setEditing(false)} />}

      <div className={styles.grid}>
        <section className={styles.card} aria-labelledby="provider-facts">
          <h3 id="provider-facts" className={styles.sectionTitle}>
            Provider
          </h3>
          <dl className={styles.facts}>
            <dt className={styles.factLabel}>ID</dt>
            <dd className={cn(styles.factValue, styles.mono)}>{p.id}</dd>
            <dt className={styles.factLabel}>Type</dt>
            <dd className={styles.factValue}>{p.type}</dd>
            <dt className={styles.factLabel}>Site</dt>
            <dd className={styles.factValue}>{site ?? "—"}</dd>
            <dt className={styles.factLabel}>Region</dt>
            <dd className={styles.factValue}>{p.regionName || "—"}</dd>
            <dt className={styles.factLabel}>Endpoints</dt>
            <dd className={cn(styles.factValue, styles.mono)}>{p.endpoints.join(", ")}</dd>
            <dt className={styles.factLabel}>Fingerprint</dt>
            <dd className={cn(styles.factValue, styles.mono)}>{p.fingerprint}</dd>
          </dl>
        </section>
        <SyncStatusCard providerId={p.id} />
        <ConnectionCard provider={p} canCheck={isAdmin} />
      </div>

      <CredentialsSection provider={p} enabled={isAdmin} />
    </div>
  );
}

function SyncStatusCard({ providerId }: { providerId: string }) {
  const status = useSyncStatus(providerId);
  return (
    <section className={styles.card} aria-labelledby="provider-sync">
      <h3 id="provider-sync" className={styles.sectionTitle}>
        Inventory sync
      </h3>
      {status.isPending && <p className={styles.muted}>Loading…</p>}
      {status.isError && <ApiErrorAlert error={status.error} />}
      {status.data === null && (
        <p className={styles.muted}>Not synced yet. The first sync runs within one sync interval after registration.</p>
      )}
      {status.data && (
        <dl className={styles.facts}>
          <dt className={styles.factLabel}>Status</dt>
          <dd className={styles.status} role="status">
            <span className={cn(styles.dot, healthDot[status.data.status])} aria-hidden="true" />
            {status.data.status}
          </dd>
          <dt className={styles.factLabel}>Last success</dt>
          <dd className={styles.factValue}>
            {status.data.lastSuccessAt ? formatAge(status.data.lastSuccessAt) : "never"}
          </dd>
          <dt className={styles.factLabel}>Last attempt</dt>
          <dd className={styles.factValue}>{formatAge(status.data.lastAttemptAt)}</dd>
          <dt className={styles.factLabel}>Failures</dt>
          <dd className={styles.factValue}>{status.data.consecutiveFailures}</dd>
          {status.data.lastError && (
            <>
              <dt className={styles.factLabel}>Last error</dt>
              <dd className={styles.errorText}>{status.data.lastError}</dd>
            </>
          )}
        </dl>
      )}
    </section>
  );
}

/** Two-step delete: the first click only arms the confirmation. */
function DeleteProvider({ providerId, name }: { providerId: string; name: string }) {
  const [armed, setArmed] = useState(false);
  const del = useDeleteProvider();
  const navigate = useNavigate();

  if (!armed) {
    return (
      <button type="button" className={styles.danger} onClick={() => setArmed(true)}>
        Remove provider
      </button>
    );
  }
  return (
    <div className={styles.confirm}>
      <span>Remove {name} and its credentials? Its inventory stops syncing.</span>
      <button
        type="button"
        className={styles.danger}
        disabled={del.isPending}
        onClick={() => del.mutate(providerId, { onSuccess: () => navigate("/settings/providers", { replace: true }) })}
      >
        {del.isPending ? "Removing…" : "Confirm remove"}
      </button>
      <button type="button" className={styles.secondary} onClick={() => setArmed(false)}>
        Cancel
      </button>
      <ApiErrorAlert error={del.error} />
    </div>
  );
}
