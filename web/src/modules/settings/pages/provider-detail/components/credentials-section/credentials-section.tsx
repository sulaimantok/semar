import { useState, type FormEvent } from "react";

import { cn } from "@/shared/utils/cn";
import { formatAge } from "@/shared/utils/format";

import { ApiErrorAlert } from "../../../../components/api-error-alert";
import { FormField } from "../../../../components/form-field";
import { CREDENTIAL_PURPOSES, SECRET_FIELDS } from "../../../../constants/settings";
import { useDeleteCredential, usePutCredential } from "../../../../hooks/use-settings-mutations";
import { useCredentials } from "../../../../hooks/use-settings-queries";
import type { ICredentialPurpose, IProvider } from "../../../../services/settings-service";

import { credentialsSectionStyles as styles } from "./credentials-section.styles";

export interface ICredentialsSectionProps {
  provider: IProvider;
  /** Credentials are admin-only; others see a note instead of a 403. */
  enabled: boolean;
}

export function CredentialsSection({ provider, enabled }: ICredentialsSectionProps) {
  const credentials = useCredentials(provider.id, enabled);
  const del = useDeleteCredential(provider.id);
  const rows = credentials.data ?? [];

  return (
    <section className={styles.section} aria-labelledby="credentials-title">
      <header>
        <h3 id="credentials-title" className={styles.sectionTitle}>
          Credentials
        </h3>
        <p className={styles.sectionHint}>
          Write-only: secrets are sealed with envelope encryption and are never shown again.
        </p>
      </header>

      {!enabled ? (
        <p className={styles.empty}>Only global admins can view or manage credentials.</p>
      ) : (
        <>
          {credentials.isError && <ApiErrorAlert error={credentials.error} />}
          {del.isError && <ApiErrorAlert error={del.error} />}
          {credentials.isPending ? (
            <p className={styles.muted}>Loading…</p>
          ) : rows.length === 0 ? (
            <p className={styles.empty}>No credentials stored. OpenStack and Proxmox providers need a discovery credential for inventory sync.</p>
          ) : (
            <div className={styles.tableWrap}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th className={styles.th}>Purpose</th>
                    <th className={styles.th}>Scope</th>
                    <th className={styles.th}>Owner</th>
                    <th className={styles.th}>Rotated</th>
                    <th className={styles.th}>Expires</th>
                    <th className={styles.th}>
                      <span className={styles.srOnly}>Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((c) => (
                    <tr key={c.id}>
                      <td className={styles.td}>{c.purpose}</td>
                      <td className={cn(styles.td, styles.mono)}>{c.scope}</td>
                      <td className={styles.td}>{c.owner || "—"}</td>
                      <td className={styles.td} title={c.rotatedAt}>
                        {formatAge(c.rotatedAt)}
                      </td>
                      <td className={styles.td}>{c.expiresAt ? new Date(c.expiresAt).toLocaleDateString() : "—"}</td>
                      <td className={cn(styles.td, styles.rowActions)}>
                        <button
                          type="button"
                          className={styles.danger}
                          disabled={del.isPending}
                          aria-label={`Delete ${c.purpose} credential for ${c.scope}`}
                          onClick={() => del.mutate({ purpose: c.purpose, scope: c.scope })}
                        >
                          Delete
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <PutCredentialForm provider={provider} />
        </>
      )}
    </section>
  );
}

function emptySecret(provider: IProvider): Record<string, string> {
  return Object.fromEntries(SECRET_FIELDS[provider.type].map((f) => [f, ""]));
}

function PutCredentialForm({ provider }: { provider: IProvider }) {
  const put = usePutCredential(provider.id);
  const [purpose, setPurpose] = useState<ICredentialPurpose>("discovery");
  const [scope, setScope] = useState("provider");
  const [owner, setOwner] = useState("");
  const [expires, setExpires] = useState("");
  const [secret, setSecret] = useState<Record<string, string>>(() => emptySecret(provider));
  const meta = CREDENTIAL_PURPOSES.find((p) => p.value === purpose)!;

  const onPurpose = (value: ICredentialPurpose) => {
    setPurpose(value);
    setScope(CREDENTIAL_PURPOSES.find((p) => p.value === value)?.fixedScope ?? "");
  };

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    put.mutate(
      {
        purpose,
        scope,
        owner: owner || undefined,
        expiresAt: expires ? new Date(`${expires}T00:00:00Z`).toISOString() : undefined,
        secret,
      },
      // Drop the secret from memory as soon as the server has it.
      { onSettled: () => setSecret(emptySecret(provider)) },
    );
  };

  return (
    <form className={styles.card} onSubmit={onSubmit} aria-labelledby="put-credential-title" autoComplete="off">
      <h4 id="put-credential-title" className={styles.sectionTitle}>
        Store or replace a credential
      </h4>
      <div className={styles.form}>
        <ApiErrorAlert error={put.error} />
        <FormField label="Purpose">
          <select className={styles.input} value={purpose} onChange={(e) => onPurpose(e.target.value as ICredentialPurpose)}>
            {CREDENTIAL_PURPOSES.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </select>
        </FormField>
        <FormField label="Scope" hint={`Allowed: ${meta.scopeHint}`}>
          <input
            className={styles.input}
            required
            readOnly={!!meta.fixedScope}
            value={scope}
            onChange={(e) => setScope(e.target.value)}
          />
        </FormField>
        <FormField label="Owner" optional hint="Account that owns the credential at the provider">
          <input className={styles.input} value={owner} onChange={(e) => setOwner(e.target.value)} />
        </FormField>
        <FormField label="Expires" optional>
          <input className={styles.input} type="date" value={expires} onChange={(e) => setExpires(e.target.value)} />
        </FormField>
        <p className={styles.secretLegend}>Secret</p>
        {Object.keys(secret).map((field) => (
          <FormField key={field} label={field} wide>
            <input
              className={styles.input}
              type="password"
              required
              autoComplete="new-password"
              spellCheck={false}
              value={secret[field]}
              onChange={(e) => setSecret((s) => ({ ...s, [field]: e.target.value }))}
            />
          </FormField>
        ))}
        <p className={styles.note}>Storing a credential with the same purpose and scope replaces the existing one.</p>
        <div className={styles.actions}>
          <button type="submit" className={styles.primary} disabled={put.isPending}>
            {put.isPending ? "Storing…" : "Store credential"}
          </button>
          {put.isSuccess && (
            <span className={styles.success} role="status">
              Stored {put.data.purpose} credential for {put.data.scope}.
            </span>
          )}
        </div>
      </div>
    </form>
  );
}
