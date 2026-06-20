-- +goose Up
CREATE TABLE audit_log (
    id     BIGSERIAL PRIMARY KEY,
    ts     BIGINT NOT NULL DEFAULT 0,
    actor  TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL DEFAULT '',
    target TEXT NOT NULL DEFAULT '',
    before JSONB NOT NULL DEFAULT '{}',
    after  JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_audit_log_ts ON audit_log(ts DESC);

-- +goose Down
DROP TABLE audit_log;
