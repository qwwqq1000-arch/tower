-- +goose Up
ALTER TABLE slots ADD COLUMN owner_id TEXT NOT NULL DEFAULT '';
-- +goose Down
ALTER TABLE slots DROP COLUMN owner_id;
