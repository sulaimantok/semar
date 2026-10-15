import { useState, type FormEvent } from "react";

import { ApiErrorAlert } from "../../../../components/api-error-alert";
import { FormField } from "../../../../components/form-field";
import { TLS_HINT } from "../../../../constants/settings";
import { useUpdateProvider } from "../../../../hooks/use-settings-mutations";
import { useSites } from "../../../../hooks/use-settings-queries";
import type { IProvider, IProviderUpdate } from "../../../../services/settings-service";

import { editProviderFormStyles as styles } from "./edit-provider-form.styles";

function lines(text: string): string[] {
  return text
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean);
}

export interface IEditProviderFormProps {
  provider: IProvider;
  onDone: () => void;
}

/** Edits a provider in place; only changed fields are sent. ID and type are fixed. */
export function EditProviderForm({ provider, onDone }: IEditProviderFormProps) {
  const sites = useSites();
  const update = useUpdateProvider(provider.id);
  const [name, setName] = useState(provider.name);
  const [siteId, setSiteId] = useState(provider.siteId ?? "");
  const [endpoints, setEndpoints] = useState(provider.endpoints.join("\n"));
  const [regionName, setRegionName] = useState(provider.regionName);
  const [tlsFingerprint, setTlsFingerprint] = useState(provider.tlsFingerprint ?? "");

  const patch: IProviderUpdate = {};
  if (name.trim() !== provider.name) patch.name = name;
  if (siteId !== (provider.siteId ?? "")) patch.siteId = siteId;
  const newEndpoints = lines(endpoints);
  if (newEndpoints.join("\n") !== provider.endpoints.join("\n")) patch.endpoints = newEndpoints;
  if (provider.type === "openstack" && regionName.trim() !== provider.regionName) patch.regionName = regionName;
  if (tlsFingerprint.trim() !== (provider.tlsFingerprint ?? "")) patch.tlsFingerprint = tlsFingerprint.trim();
  const changed = Object.keys(patch).length > 0;
  const addressChanged = patch.endpoints !== undefined || patch.regionName !== undefined;

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (changed) update.mutate(patch, { onSuccess: onDone });
  };

  return (
    <form className={styles.card} onSubmit={onSubmit} aria-labelledby="edit-provider-title">
      <h3 id="edit-provider-title" className={styles.sectionTitle}>
        Edit provider
      </h3>
      <div className={styles.form}>
        <ApiErrorAlert error={update.error} />
        <FormField label="Name">
          <input className={styles.input} required value={name} onChange={(e) => setName(e.target.value)} />
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
        <FormField label="Endpoints" wide hint="One URL per line">
          <textarea className={styles.textarea} required value={endpoints} onChange={(e) => setEndpoints(e.target.value)} />
        </FormField>
        {provider.type === "openstack" && (
          <FormField label="Region">
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
        {addressChanged && (
          <p className={styles.note} role="note">
            Changing the address clears the last connection check; run it again after saving.
          </p>
        )}
        {patch.siteId !== undefined && (
          <p className={styles.note} role="note">
            Inventory sync moves to the worker of the new site within one reconcile interval.
          </p>
        )}
        <div className={styles.actions}>
          <button type="submit" className={styles.primary} disabled={!changed || update.isPending}>
            {update.isPending ? "Saving…" : "Save changes"}
          </button>
          <button type="button" className={styles.secondary} onClick={onDone}>
            Cancel
          </button>
        </div>
      </div>
    </form>
  );
}
