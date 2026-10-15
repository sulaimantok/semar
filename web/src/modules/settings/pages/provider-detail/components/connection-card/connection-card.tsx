import { cn } from "@/shared/utils/cn";
import { formatAge } from "@/shared/utils/format";

import { ApiErrorAlert } from "../../../../components/api-error-alert";
import { useValidateProvider } from "../../../../hooks/use-settings-mutations";
import type { IProvider } from "../../../../services/settings-service";

import { connectionCardStyles as styles } from "./connection-card.styles";

export interface IConnectionCardProps {
  provider: IProvider;
  /** Running a check is admin-only; others see the last result. */
  canCheck: boolean;
}

export function ConnectionCard({ provider: p, canCheck }: IConnectionCardProps) {
  const check = useValidateProvider(p.id);
  const versions = Object.entries(p.apiVersions ?? {});

  return (
    <section className={styles.card} aria-labelledby="provider-connection">
      <h3 id="provider-connection" className={styles.sectionTitle}>
        Connection
      </h3>
      {p.validatedAt ? (
        <dl className={styles.facts}>
          <dt className={styles.factLabel}>Last check</dt>
          <dd className={styles.factValue} title={p.validatedAt}>
            {formatAge(p.validatedAt)}
          </dd>
          <dt className={styles.factLabel}>Cloud identity</dt>
          <dd className={cn(styles.factValue, styles.mono)}>{p.cloudFingerprint}</dd>
          <dt className={styles.factLabel}>API versions</dt>
          <dd className={styles.factValue}>
            {versions.length ? (
              <span className={styles.versions}>
                {versions.map(([svc, v]) => (
                  <span key={svc} className={styles.badge}>
                    {svc} {v}
                  </span>
                ))}
              </span>
            ) : (
              "—"
            )}
          </dd>
        </dl>
      ) : (
        <p className={styles.muted}>Not checked since the provider was registered or its address changed.</p>
      )}
      {canCheck && (
        <>
          <ApiErrorAlert error={check.error} />
          <div>
            <button type="button" className={styles.secondary} disabled={check.isPending} onClick={() => check.mutate()}>
              {check.isPending ? "Checking…" : "Check connection"}
            </button>
          </div>
          {check.isSuccess && (
            <p className={styles.success} role="status">
              Connected. The cloud identity and API versions were recorded.
            </p>
          )}
        </>
      )}
    </section>
  );
}
