import { useState, type FormEvent } from "react";
import { Link } from "react-router";

import { useIsAdmin } from "@/modules/auth";

import { ApiErrorAlert } from "../../components/api-error-alert";
import { FormField } from "../../components/form-field";
import { ENTITY_ID_HINT, ENTITY_ID_PATTERN, PROVIDER_TYPES, TLS_HINT } from "../../constants/settings";
import { useCreateProvider } from "../../hooks/use-settings-mutations";
import { useSettingsProviders, useSites } from "../../hooks/use-settings-queries";
import type { IProviderType } from "../../services/settings-service";

import { providersStyles as styles } from "./providers.styles";

export function ProvidersPage() {
  const isAdmin = useIsAdmin();
  const providers = useSettingsProviders();
  const sites = useSites();
  const siteName = new Map((sites.data ?? []).map((s) => [s.id, s.name]));
  const rows = providers.data ?? [];

  return (
    <section className={styles.section} aria-labelledby="providers-title">
      <header className={styles.sectionHeader}>
        <div>
          <h2 id="providers-title" className={styles.sectionTitle}>
            Providers
          </h2>
          <p className={styles.sectionHint}>
            One OpenStack region or one Proxmox cluster each. Registering the same cloud twice is refused.
          </p>
        </div>
      </header>

      {providers.isError && <ApiErrorAlert error={providers.error} />}
      {providers.isPending ? (
        <p className={styles.muted}>Loading…</p>
      ) : rows.length === 0 ? (
        <p className={styles.empty}>No providers registered yet.</p>
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th className={styles.th}>Name</th>
                <th className={styles.th}>Type</th>
                <th className={styles.th}>Site</th>
                <th className={styles.th}>Endpoints</th>
                <th className={styles.th}>Region</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((p) => (
                <tr key={p.id}>
                  <td className={styles.td}>
                    <Link className={styles.link} to={`/settings/providers/${p.id}`}>
                      {p.name}
                    </Link>
                    <div className={styles.mono}>{p.id}</div>
                  </td>
                  <td className={styles.td}>{p.type}</td>
                  <td className={styles.td}>
                    {p.siteId ? (siteName.get(p.siteId) ?? p.siteId) : <span className={styles.badge}>no site</span>}
                  </td>
                  <td className={styles.td}>
                    <div className={styles.endpoints}>
                      {p.endpoints.map((e) => (
                        <span key={e}>{e}</span>
                      ))}
                    </div>
                  </td>
                  <td className={styles.td}>{p.regionName || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {isAdmin && <CreateProviderForm />}
    </section>
  );
}

function CreateProviderForm() {
  const sites = useSites();
  const create = useCreateProvider();
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [type, setType] = useState<IProviderType>("openstack");
  const [siteId, setSiteId] = useState("");
  const [endpoints, setEndpoints] = useState("");
  const [regionName, setRegionName] = useState("");
  const [tlsFingerprint, setTlsFingerprint] = useState("");

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate(
      {
        id,
        name,
        type,
        siteId: siteId || undefined,
        endpoints: endpoints
          .split("\n")
          .map((s) => s.trim())
          .filter(Boolean),
        regionName: type === "openstack" ? regionName : undefined,
        tlsFingerprint: tlsFingerprint.trim() || undefined,
      },
      {
        onSuccess: () => {
          setId("");
          setName("");
          setEndpoints("");
          setRegionName("");
          setTlsFingerprint("");
        },
      },
    );
  };

  return (
    <form className={styles.card} onSubmit={onSubmit} aria-labelledby="create-provider-title">
      <h3 id="create-provider-title" className={styles.sectionTitle}>
        Register a provider
      </h3>
      <div className={styles.form}>
        <ApiErrorAlert error={create.error} />
        <FormField label="ID" hint={ENTITY_ID_HINT}>
          <input className={styles.input} required pattern={ENTITY_ID_PATTERN} value={id} onChange={(e) => setId(e.target.value)} />
        </FormField>
        <FormField label="Name">
          <input className={styles.input} required value={name} onChange={(e) => setName(e.target.value)} />
        </FormField>
        <FormField label="Type">
          <select className={styles.input} value={type} onChange={(e) => setType(e.target.value as IProviderType)}>
            {PROVIDER_TYPES.map((t) => (
              <option key={t.value} value={t.value}>
                {t.label}
              </option>
            ))}
          </select>
        </FormField>
        <FormField label="Site" optional>
          <select className={styles.input} value={siteId} onChange={(e) => setSiteId(e.target.value)}>
            <option value="">No site</option>
            {(sites.data ?? []).map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </FormField>
        <FormField
          label="Endpoints"
          wide
          hint={type === "openstack" ? "Keystone URL, e.g. https://keystone.example.com:5000/v3" : "One API URL per line, for every node of the cluster"}
        >
          <textarea className={styles.textarea} required value={endpoints} onChange={(e) => setEndpoints(e.target.value)} />
        </FormField>
        {type === "openstack" && (
          <FormField label="Region" hint="Each region is registered as its own provider.">
            <input className={styles.input} required value={regionName} onChange={(e) => setRegionName(e.target.value)} />
          </FormField>
        )}
        <FormField label="TLS fingerprint" optional wide hint={TLS_HINT}>
          <input
            className={styles.input}
            spellCheck={false}
            value={tlsFingerprint}
            onChange={(e) => setTlsFingerprint(e.target.value)}
          />
        </FormField>
        <div className={styles.actions}>
          <button type="submit" className={styles.primary} disabled={create.isPending}>
            {create.isPending ? "Registering…" : "Register provider"}
          </button>
          {create.isSuccess && (
            <span className={styles.success} role="status">
              Registered {create.data.name}. Open it to add credentials and follow its first sync.
            </span>
          )}
        </div>
      </div>
    </form>
  );
}
