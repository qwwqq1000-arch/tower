-- +goose Up
ALTER TABLE tenants ADD COLUMN channel_rate DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE tenants ADD COLUMN fallback_limit INTEGER NOT NULL DEFAULT 1;
-- +goose Down
ALTER TABLE tenants DROP COLUMN channel_rate; ALTER TABLE tenants DROP COLUMN fallback_limit;
