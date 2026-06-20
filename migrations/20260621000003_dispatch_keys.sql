-- +goose Up
CREATE TABLE dispatch_keys (
    id         TEXT PRIMARY KEY,
    key_hash   TEXT NOT NULL,
    salt       TEXT NOT NULL,
    prefix     TEXT NOT NULL,
    owner_id   TEXT NOT NULL DEFAULT '',
    label      TEXT NOT NULL DEFAULT '',
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_dispatch_keys_prefix ON dispatch_keys(prefix);

-- +goose Down
DROP TABLE dispatch_keys;
