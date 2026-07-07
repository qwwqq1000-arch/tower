-- +goose Up
ALTER TABLE dispatch_logs ADD COLUMN is_attempt BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose Down
ALTER TABLE dispatch_logs DROP COLUMN is_attempt;
