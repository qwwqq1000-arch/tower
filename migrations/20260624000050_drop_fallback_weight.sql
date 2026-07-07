-- +goose Up
ALTER TABLE fallback_channels DROP COLUMN IF EXISTS weight;
-- +goose Down
ALTER TABLE fallback_channels ADD COLUMN weight INTEGER NOT NULL DEFAULT 1;
