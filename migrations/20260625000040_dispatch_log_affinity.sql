-- +goose Up
ALTER TABLE dispatch_logs ADD COLUMN affinity_hit BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose Down
ALTER TABLE dispatch_logs DROP COLUMN affinity_hit;
