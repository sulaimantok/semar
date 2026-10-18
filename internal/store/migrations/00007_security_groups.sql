-- Security groups join the inventory cache as their own resource kind.

-- +goose Up
ALTER TABLE resources DROP CONSTRAINT resources_kind_check;
ALTER TABLE resources ADD CONSTRAINT resources_kind_check
    CHECK (kind IN ('hypervisor', 'vm', 'image', 'network', 'volume', 'security_group'));

-- +goose Down
DELETE FROM resources WHERE kind = 'security_group';
ALTER TABLE resources DROP CONSTRAINT resources_kind_check;
ALTER TABLE resources ADD CONSTRAINT resources_kind_check
    CHECK (kind IN ('hypervisor', 'vm', 'image', 'network', 'volume'));
