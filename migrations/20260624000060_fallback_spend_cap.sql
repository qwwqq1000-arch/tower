-- +goose Up
ALTER TABLE fallback_channels
  ADD COLUMN spend_cap_daily_min_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
  ADD COLUMN spend_cap_daily_max_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
  ADD COLUMN spend_cap_total_min_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
  ADD COLUMN spend_cap_total_max_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
  ADD COLUMN spend_cap_action TEXT NOT NULL DEFAULT 'skip';
-- +goose Down
ALTER TABLE fallback_channels
  DROP COLUMN spend_cap_daily_min_usd, DROP COLUMN spend_cap_daily_max_usd,
  DROP COLUMN spend_cap_total_min_usd, DROP COLUMN spend_cap_total_max_usd,
  DROP COLUMN spend_cap_action;
