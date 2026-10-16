-- Delivery cursor of each external audit sink (roadmap §3.4, tracker 1.1):
-- the ID of the last event a sink acknowledged. Delivery is at least once;
-- receivers deduplicate by event ID and can verify the hash chain.

-- +goose Up
CREATE TABLE audit_sink_cursors (
    sink       text PRIMARY KEY,
    last_id    bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE audit_sink_cursors;
