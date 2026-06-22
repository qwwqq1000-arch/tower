-- +goose Up
ALTER TABLE nodes ADD COLUMN kind TEXT NOT NULL DEFAULT 'meridian';

-- +goose Down
ALTER TABLE nodes DROP COLUMN kind;
