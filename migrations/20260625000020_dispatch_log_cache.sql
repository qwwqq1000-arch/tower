-- +goose Up
ALTER TABLE dispatch_logs ADD COLUMN cache_read BIGINT NOT NULL DEFAULT 0;
ALTER TABLE dispatch_logs ADD COLUMN cache_creation BIGINT NOT NULL DEFAULT 0;
-- +goose Down
ALTER TABLE dispatch_logs DROP COLUMN cache_read;
ALTER TABLE dispatch_logs DROP COLUMN cache_creation;
