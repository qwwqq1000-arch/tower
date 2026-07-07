-- +goose Up
ALTER TABLE fallback_channels ADD COLUMN weight INTEGER NOT NULL DEFAULT 1;
-- +goose Down
ALTER TABLE fallback_channels DROP COLUMN weight;
