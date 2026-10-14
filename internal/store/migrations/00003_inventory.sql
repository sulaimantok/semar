-- Inventory cache (roadmap §3.6). Providers remain the source of truth;
-- these tables hold the last observed state plus an append-only history.

-- +goose Up
CREATE TABLE resources (
    provider_id          text NOT NULL REFERENCES providers (id) ON DELETE CASCADE,
    kind                 text NOT NULL CHECK (kind IN ('hypervisor', 'vm', 'image', 'network', 'volume')),
    external_id          text NOT NULL CHECK (external_id <> ''),
    name                 text NOT NULL DEFAULT '',
    external_scope       text NOT NULL DEFAULT '',
    -- Derived from project_mappings on every sync; NULL = unassigned.
    project_id           text REFERENCES projects (id) ON DELETE SET NULL,
    arch                 text NOT NULL DEFAULT 'unknown' CHECK (arch IN ('amd64', 'arm64', 'unknown')),
    status               text NOT NULL DEFAULT '',
    -- Kind-specific attributes as reported by the driver.
    spec                 jsonb NOT NULL DEFAULT '{}',
    spec_hash            bytea NOT NULL,
    observed_at          timestamptz NOT NULL,
    first_seen_at        timestamptz NOT NULL,
    missed_syncs         integer NOT NULL DEFAULT 0,
    orphaned_at          timestamptz,
    -- Lease held by a workflow mutating the resource; sync leaves it alone.
    pending_operation_id text,
    PRIMARY KEY (provider_id, kind, external_id)
);

CREATE INDEX resources_active ON resources (kind, provider_id) WHERE orphaned_at IS NULL;
CREATE INDEX resources_project ON resources (project_id) WHERE orphaned_at IS NULL;

CREATE TABLE resource_changes (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider_id text NOT NULL,
    kind        text NOT NULL,
    external_id text NOT NULL,
    change      text NOT NULL CHECK (change IN ('created', 'updated', 'orphaned', 'restored')),
    observed_at timestamptz NOT NULL,
    project_id  text,
    spec        jsonb NOT NULL DEFAULT '{}'
);

CREATE INDEX resource_changes_resource ON resource_changes (provider_id, kind, external_id, id);

CREATE TABLE provider_sync_status (
    provider_id          text PRIMARY KEY REFERENCES providers (id) ON DELETE CASCADE,
    status               text NOT NULL CHECK (status IN ('healthy', 'degraded', 'unreachable')),
    last_attempt_at      timestamptz NOT NULL,
    last_success_at      timestamptz,
    -- observed_at of the newest snapshot applied; older snapshots are stale.
    last_observed_at     timestamptz,
    consecutive_failures integer NOT NULL DEFAULT 0,
    last_error           text NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE provider_sync_status;
DROP TABLE resource_changes;
DROP TABLE resources;
