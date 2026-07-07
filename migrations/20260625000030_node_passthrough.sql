-- +goose Up
ALTER TABLE nodes ADD COLUMN passthrough BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose Down
ALTER TABLE nodes DROP COLUMN passthrough;
