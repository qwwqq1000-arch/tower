-- +goose Up
CREATE TABLE provision_jobs (
    id         TEXT PRIMARY KEY,
    host       TEXT NOT NULL DEFAULT '',
    name       TEXT NOT NULL DEFAULT '',
    owner_id   TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'running',
    step       TEXT NOT NULL DEFAULT '',
    log        TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL DEFAULT 0,
    updated_at BIGINT NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE provision_jobs;
