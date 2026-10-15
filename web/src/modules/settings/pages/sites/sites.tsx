import { useState, type FormEvent } from "react";

import { useIsAdmin } from "@/modules/auth";

import { ApiErrorAlert } from "../../components/api-error-alert";
import { FormField } from "../../components/form-field";
import { ENTITY_ID_HINT, ENTITY_ID_PATTERN } from "../../constants/settings";
import { useCreateSite } from "../../hooks/use-settings-mutations";
import { useSites } from "../../hooks/use-settings-queries";
import { parseLabels } from "../../utils/labels";

import { sitesStyles as styles } from "./sites.styles";

export function SitesPage() {
  const isAdmin = useIsAdmin();
  const sites = useSites();
  const rows = sites.data ?? [];

  return (
    <section className={styles.section} aria-labelledby="sites-title">
      <header>
        <h2 id="sites-title" className={styles.sectionTitle}>
          Sites
        </h2>
        <p className={styles.sectionHint}>Physical locations. Used for filtering and failure domains, not for permissions.</p>
      </header>

      {sites.isError && <ApiErrorAlert error={sites.error} />}
      {sites.isPending ? (
        <p className={styles.muted}>Loading…</p>
      ) : rows.length === 0 ? (
        <p className={styles.empty}>No sites yet.</p>
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th className={styles.th}>Name</th>
                <th className={styles.th}>ID</th>
                <th className={styles.th}>Labels</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => (
                <tr key={s.id}>
                  <td className={styles.td}>
                    <span className={styles.name}>{s.name}</span>
                  </td>
                  <td className={styles.td}>
                    <span className={styles.mono}>{s.id}</span>
                  </td>
                  <td className={styles.td}>
                    <div className={styles.labels}>
                      {Object.entries(s.labels).map(([k, v]) => (
                        <span key={k} className={styles.badge}>
                          {k}={v}
                        </span>
                      ))}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {isAdmin && <CreateSiteForm />}
    </section>
  );
}

function CreateSiteForm() {
  const create = useCreateSite();
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [labels, setLabels] = useState("");
  const parsed = parseLabels(labels);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!parsed) return;
    create.mutate(
      { id, name, labels: parsed },
      {
        onSuccess: () => {
          setId("");
          setName("");
          setLabels("");
        },
      },
    );
  };

  return (
    <form className={styles.card} onSubmit={onSubmit} aria-labelledby="create-site-title">
      <h3 id="create-site-title" className={styles.sectionTitle}>
        Add a site
      </h3>
      <div className={styles.form}>
        <ApiErrorAlert error={create.error} />
        <FormField label="ID" hint={ENTITY_ID_HINT}>
          <input className={styles.input} required pattern={ENTITY_ID_PATTERN} value={id} onChange={(e) => setId(e.target.value)} />
        </FormField>
        <FormField label="Name">
          <input className={styles.input} required value={name} onChange={(e) => setName(e.target.value)} />
        </FormField>
        <FormField label="Labels" optional wide hint={parsed ? "key=value, separated by commas" : "Each label must be key=value"}>
          <input className={styles.input} aria-invalid={!parsed} value={labels} onChange={(e) => setLabels(e.target.value)} />
        </FormField>
        <div className={styles.actions}>
          <button type="submit" className={styles.primary} disabled={create.isPending || !parsed}>
            {create.isPending ? "Adding…" : "Add site"}
          </button>
        </div>
      </div>
    </form>
  );
}
