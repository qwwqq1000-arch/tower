-- +goose Up
CREATE TABLE dispatch_events (
    id       BIGSERIAL PRIMARY KEY,
    ts       BIGINT NOT NULL DEFAULT 0,
    type     TEXT NOT NULL DEFAULT '',
    target   TEXT NOT NULL DEFAULT '',
    owner_id TEXT NOT NULL DEFAULT '',
    detail   JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_dispatch_events_ts ON dispatch_events(ts DESC);

CREATE TABLE ban_episodes (
    id           BIGSERIAL PRIMARY KEY,
    node_id      TEXT NOT NULL DEFAULT '',
    profile_id   TEXT NOT NULL DEFAULT '',
    banned_at    BIGINT NOT NULL DEFAULT 0,
    recovered_at BIGINT NOT NULL DEFAULT 0,
    survival_ms  BIGINT NOT NULL DEFAULT 0,
    detail       JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_ban_episodes_node ON ban_episodes(node_id, profile_id);

-- +goose Down
DROP TABLE ban_episodes;
DROP TABLE dispatch_events;
