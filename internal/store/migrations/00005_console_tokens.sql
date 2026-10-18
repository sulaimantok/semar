-- SEMAR console tokens (roadmap §3.4): single-use, short-lived, bound to
-- the user, the login session and the VM. Only the token's hash is stored.
-- In the database rather than in memory so that the WebSocket may reach
-- another API replica than the one that issued the token.

-- +goose Up
CREATE TABLE console_tokens (
    token_hash   bytea PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    session_hash bytea NOT NULL,
    provider_id  text NOT NULL,
    external_id  text NOT NULL,
    kind         text NOT NULL CHECK (kind IN ('vnc', 'serial')),
    expires_at   timestamptz NOT NULL,
    used_at      timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX console_tokens_expires ON console_tokens (expires_at);

-- +goose Down
DROP TABLE console_tokens;
