-- Connection checks (roadmap §3.5): the driver reports the identity of the
-- cloud behind a provider. Unlike the endpoint-derived fingerprint, it does
-- not change when a URL is renamed, so it also catches one cloud registered
-- under two different URLs.

-- +goose Up
ALTER TABLE providers
    ADD COLUMN cloud_fingerprint text CHECK (cloud_fingerprint <> ''),
    ADD COLUMN validated_at      timestamptz;

CREATE UNIQUE INDEX providers_cloud_fingerprint_active ON providers (cloud_fingerprint)
    WHERE deleted_at IS NULL AND cloud_fingerprint IS NOT NULL;

-- +goose Down
DROP INDEX providers_cloud_fingerprint_active;
ALTER TABLE providers DROP COLUMN validated_at, DROP COLUMN cloud_fingerprint;
