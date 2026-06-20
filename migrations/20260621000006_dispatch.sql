-- +goose Up
CREATE TABLE policies (
    scope_type TEXT NOT NULL,
    scope_id   TEXT NOT NULL,
    params     JSONB NOT NULL DEFAULT '{}',
    updated_at BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (scope_type, scope_id)
);

CREATE TABLE fallback_channels (
    id              TEXT PRIMARY KEY,
    owner_id        TEXT NOT NULL DEFAULT '',
    group_id        TEXT NOT NULL DEFAULT '',
    name            TEXT NOT NULL DEFAULT '',
    base_url        TEXT NOT NULL,
    api_key         TEXT NOT NULL DEFAULT '',
    priority        INTEGER NOT NULL DEFAULT 100,
    weight          INTEGER NOT NULL DEFAULT 100,
    max_concurrent  INTEGER NOT NULL DEFAULT 0,
    cooldown_ms     BIGINT NOT NULL DEFAULT 0,
    price_threshold DOUBLE PRECISION NOT NULL DEFAULT 0,
    model_allowlist TEXT NOT NULL DEFAULT '',
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE dispatch_logs (
    id              BIGSERIAL PRIMARY KEY,
    ts              BIGINT NOT NULL DEFAULT 0,
    owner_id        TEXT NOT NULL DEFAULT '',
    model           TEXT NOT NULL DEFAULT '',
    target          TEXT NOT NULL DEFAULT '',
    profile_id      TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT '',
    http_status     INTEGER NOT NULL DEFAULT 0,
    latency_ms      BIGINT NOT NULL DEFAULT 0,
    tokens_in       BIGINT NOT NULL DEFAULT 0,
    tokens_out      BIGINT NOT NULL DEFAULT 0,
    fallback_reason TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_dispatch_logs_ts ON dispatch_logs(ts DESC);

-- +goose Down
DROP TABLE dispatch_logs;
DROP TABLE fallback_channels;
DROP TABLE policies;
