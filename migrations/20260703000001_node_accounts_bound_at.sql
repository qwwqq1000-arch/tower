-- +goose Up
ALTER TABLE node_accounts ADD COLUMN IF NOT EXISTS bound_at bigint NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE node_accounts DROP COLUMN IF EXISTS bound_at;
