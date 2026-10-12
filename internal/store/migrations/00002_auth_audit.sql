-- Local users, sessions, role bindings and the append-only audit log
-- (roadmap §3.4). Migrations must stay backward-compatible.

-- +goose Up
CREATE TABLE users (
    id            uuid PRIMARY KEY,
    username      text NOT NULL UNIQUE CHECK (username ~ '^[a-z0-9][a-z0-9._-]{0,62}$'),
    password_hash text NOT NULL,
    disabled      boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- Only a SHA-256 hash of the session token is stored.
CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX sessions_user ON sessions (user_id);
CREATE INDEX sessions_expires ON sessions (expires_at);

-- NULL project/provider = global binding.
CREATE TABLE role_bindings (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group')),
    subject_id   text NOT NULL CHECK (subject_id <> ''),
    role         text NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    project_id   text REFERENCES projects (id) ON DELETE CASCADE,
    provider_id  text REFERENCES providers (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (subject_type, subject_id, role, project_id, provider_id)
);

CREATE INDEX role_bindings_subject ON role_bindings (subject_type, subject_id);

-- Append-only audit log with a hash chain: hash = SHA-256(prev_hash || event).
CREATE TABLE audit_events (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    occurred_at timestamptz NOT NULL,
    actor       text NOT NULL,
    action      text NOT NULL,
    target      text NOT NULL DEFAULT '',
    outcome     text NOT NULL CHECK (outcome IN ('requested', 'success', 'failure')),
    request_id  text NOT NULL DEFAULT '',
    details     jsonb NOT NULL DEFAULT '{}',
    prev_hash   bytea NOT NULL,
    hash        bytea NOT NULL UNIQUE
);

-- +goose StatementBegin
CREATE FUNCTION audit_events_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only' USING ERRCODE = 'insufficient_privilege';
END
$$;
-- +goose StatementEnd

-- Triggers fire for every role, including the table owner. A superuser can
-- still disable them, so production runs semar-api as a non-superuser role.
CREATE TRIGGER audit_events_no_update_delete
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_append_only();

CREATE TRIGGER audit_events_no_truncate
    BEFORE TRUNCATE ON audit_events
    FOR EACH STATEMENT EXECUTE FUNCTION audit_events_append_only();

-- +goose Down
DROP TABLE audit_events;
DROP FUNCTION audit_events_append_only();
DROP TABLE role_bindings;
DROP TABLE sessions;
DROP TABLE users;
