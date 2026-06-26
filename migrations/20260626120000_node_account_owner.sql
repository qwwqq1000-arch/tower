-- +goose Up
ALTER TABLE nodes ADD COLUMN account_owner_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE nodes DROP COLUMN account_owner_id;
