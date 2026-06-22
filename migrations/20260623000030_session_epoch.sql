-- +goose Up
ALTER TABLE tenants ADD COLUMN session_epoch BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE tenants DROP COLUMN session_epoch;
