-- +goose Up
-- Per-request detail (full request body + redacted headers) for the dispatch log
-- "view request" feature (logs-detail-1). Stored ONE row per request (not per log
-- row) in a separate table so the hot dispatch_logs list stays lean; log rows carry
-- a lightweight request_id that links to the detail. Pruned independently on a short
-- retention so request bodies never bloat the database (nexaxis-disk-wal-bloat).
ALTER TABLE dispatch_logs ADD COLUMN request_id TEXT NOT NULL DEFAULT '';

CREATE TABLE dispatch_log_details (
    request_id  TEXT PRIMARY KEY,
    owner_id    TEXT NOT NULL DEFAULT '', -- for tenant-scoped access in the detail endpoint
    ts          BIGINT NOT NULL DEFAULT 0,
    req_body    TEXT NOT NULL DEFAULT '', -- capped + as-sent request body
    req_headers TEXT NOT NULL DEFAULT ''  -- JSON of forwardable headers, secrets redacted
);
CREATE INDEX idx_dispatch_log_details_ts ON dispatch_log_details(ts);

-- +goose Down
DROP TABLE dispatch_log_details;
ALTER TABLE dispatch_logs DROP COLUMN request_id;
