-- +goose Up
CREATE INDEX IF NOT EXISTS idx_dispatch_logs_target_cost ON dispatch_logs (target, status) INCLUDE (cost_usd);
CREATE INDEX IF NOT EXISTS idx_dispatch_logs_ts_target ON dispatch_logs (ts, target) INCLUDE (cost_usd, status);

-- +goose Down
DROP INDEX IF EXISTS idx_dispatch_logs_target_cost;
DROP INDEX IF EXISTS idx_dispatch_logs_ts_target;
