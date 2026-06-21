-- +goose Up
ALTER TABLE dispatch_logs ADD COLUMN ttfb_ms BIGINT NOT NULL DEFAULT 0;
-- +goose Down
ALTER TABLE dispatch_logs DROP COLUMN ttfb_ms;
