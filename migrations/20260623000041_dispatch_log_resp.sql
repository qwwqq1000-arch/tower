-- +goose Up
-- Store the final response status + body alongside the request, so the log
-- "view request" modal can show WHY a request failed (the actual error message,
-- e.g. a 400's body), not just the request (logs-detail-2).
ALTER TABLE dispatch_log_details ADD COLUMN resp_status INTEGER NOT NULL DEFAULT 0;
ALTER TABLE dispatch_log_details ADD COLUMN resp_body TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE dispatch_log_details DROP COLUMN resp_status;
ALTER TABLE dispatch_log_details DROP COLUMN resp_body;
