import type { ICredentialPurpose, IProviderType } from "../services/settings-service";

/** Server-side EntityId pattern, mirrored for early form feedback. */
export const ENTITY_ID_PATTERN = "^[a-z0-9][a-z0-9-]{0,62}$";
export const ENTITY_ID_HINT = "Lowercase letters, digits and dashes; starts with a letter or digit.";

export const PROVIDER_TYPES: readonly { value: IProviderType; label: string }[] = [
  { value: "openstack", label: "OpenStack" },
  { value: "proxmox", label: "Proxmox VE" },
  { value: "fake", label: "Fake (testing)" },
];

/** What external scope means for each provider type (project mappings). */
export const SCOPE_HINT: Record<IProviderType, string> = {
  openstack: "OpenStack project ID",
  proxmox: "pool:<name>",
  fake: "e.g. proj-a",
};

/** Purposes and the scopes the API accepts for each (ADR-0005). */
export const CREDENTIAL_PURPOSES: readonly { value: ICredentialPurpose; label: string; scopeHint: string; fixedScope?: string }[] = [
  { value: "discovery", label: "Discovery (read-only inventory)", scopeHint: "provider", fixedScope: "provider" },
  { value: "mutate", label: "Mutate (VM lifecycle)", scopeHint: "project:<id> or pool:<name>" },
  { value: "capi", label: "Cluster API", scopeHint: "project:<id>" },
  { value: "cluster", label: "Cluster", scopeHint: "cluster:<id>" },
];

/** Secret field names expected by each provider type's driver. */
export const SECRET_FIELDS: Record<IProviderType, readonly string[]> = {
  openstack: ["application_credential_id", "application_credential_secret"],
  proxmox: ["token_id", "token_secret"],
  fake: ["token"],
};
