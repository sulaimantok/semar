-- Location (Site → Provider) and ownership (Project) model, roadmap §3.5.
-- Migrations must stay backward-compatible with the previous release.

-- +goose Up
CREATE TABLE sites (
    id         text PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9-]{0,62}$'),
    name       text NOT NULL CHECK (name <> ''),
    labels     jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE projects (
    id         text PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9-]{0,62}$'),
    name       text NOT NULL CHECK (name <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE providers (
    id                text PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9-]{0,62}$'),
    type              text NOT NULL CHECK (type IN ('openstack', 'proxmox', 'fake')),
    name              text NOT NULL CHECK (name <> ''),
    site_id           text REFERENCES sites (id) ON DELETE RESTRICT,
    endpoints         text[] NOT NULL CHECK (cardinality(endpoints) > 0),
    region_name       text NOT NULL DEFAULT '',
    fingerprint       text NOT NULL CHECK (fingerprint <> ''),
    tls_fingerprint   text NOT NULL DEFAULT '',
    capabilities      jsonb NOT NULL DEFAULT '{}',
    max_microversions jsonb NOT NULL DEFAULT '{}',
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz
);

-- The same cloud cannot be registered twice while active (re-registration
-- is an update, roadmap §3.5).
CREATE UNIQUE INDEX providers_fingerprint_active ON providers (fingerprint) WHERE deleted_at IS NULL;
CREATE INDEX providers_site ON providers (site_id) WHERE deleted_at IS NULL;

-- Sealed provider credentials (ADR-0004, ADR-0005). Plaintext never reaches
-- the database; AAD binds each ciphertext to (table, id, purpose).
CREATE TABLE provider_credentials (
    id          uuid PRIMARY KEY,
    provider_id text NOT NULL REFERENCES providers (id) ON DELETE CASCADE,
    purpose     text NOT NULL CHECK (purpose IN ('discovery', 'mutate', 'capi', 'cluster')),
    scope       text NOT NULL CHECK (scope <> ''),
    owner       text NOT NULL DEFAULT '',
    kek_version text NOT NULL,
    wrapped_dek bytea NOT NULL,
    ciphertext  bytea NOT NULL,
    expires_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    rotated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider_id, purpose, scope)
);

CREATE INDEX provider_credentials_kek ON provider_credentials (kek_version);

CREATE TABLE project_mappings (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id     text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    provider_id    text NOT NULL REFERENCES providers (id) ON DELETE CASCADE,
    external_scope text NOT NULL CHECK (external_scope <> ''),
    created_at     timestamptz NOT NULL DEFAULT now(),
    -- One external scope maps to at most one SEMAR Project.
    UNIQUE (provider_id, external_scope)
);

CREATE INDEX project_mappings_project ON project_mappings (project_id);

-- +goose Down
DROP TABLE project_mappings;
DROP TABLE provider_credentials;
DROP TABLE providers;
DROP TABLE projects;
DROP TABLE sites;
