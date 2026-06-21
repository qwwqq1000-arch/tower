-- +goose Up
ALTER TABLE fallback_channels ADD COLUMN balance_token TEXT NOT NULL DEFAULT '';
ALTER TABLE fallback_channels ADD COLUMN balance_user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE fallback_channels ADD COLUMN balance_alert_usd DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE fallback_channels ADD COLUMN balance_usd DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE fallback_channels ADD COLUMN balance_checked_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE fallback_channels ADD COLUMN balance_error TEXT NOT NULL DEFAULT '';
-- +goose Down
ALTER TABLE fallback_channels DROP COLUMN balance_token; ALTER TABLE fallback_channels DROP COLUMN balance_user_id; ALTER TABLE fallback_channels DROP COLUMN balance_alert_usd; ALTER TABLE fallback_channels DROP COLUMN balance_usd; ALTER TABLE fallback_channels DROP COLUMN balance_checked_at; ALTER TABLE fallback_channels DROP COLUMN balance_error;
