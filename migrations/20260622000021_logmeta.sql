-- +goose Up
ALTER TABLE dispatch_logs ADD COLUMN stream BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE dispatch_logs ADD COLUMN cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0;
-- +goose Down
ALTER TABLE dispatch_logs DROP COLUMN cost_usd;
ALTER TABLE dispatch_logs DROP COLUMN stream;
