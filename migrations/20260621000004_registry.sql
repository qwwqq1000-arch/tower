-- +goose Up
CREATE TABLE nodes (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL DEFAULT '',
    base_url         TEXT NOT NULL,
    api_key          TEXT NOT NULL DEFAULT '',
    mgmt_key         TEXT NOT NULL DEFAULT '',
    owner_id         TEXT NOT NULL DEFAULT '',
    group_id         TEXT NOT NULL DEFAULT '',
    region           TEXT NOT NULL DEFAULT '',
    short_id         TEXT NOT NULL DEFAULT '',
    version          TEXT NOT NULL DEFAULT '',
    fingerprint_seed TEXT NOT NULL DEFAULT '',
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE accounts (
    id                TEXT PRIMARY KEY,
    owner_id          TEXT NOT NULL DEFAULT '',
    email             TEXT NOT NULL DEFAULT '',
    subscription_type TEXT NOT NULL DEFAULT '',
    oauth_access_enc  TEXT NOT NULL DEFAULT '',
    oauth_refresh_enc TEXT NOT NULL DEFAULT '',
    expires_at        BIGINT NOT NULL DEFAULT 0,
    status            TEXT NOT NULL DEFAULT 'active',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    onboarded_at      BIGINT NOT NULL DEFAULT 0,
    banned_at         BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE node_accounts (
    node_id    TEXT NOT NULL,
    account_id TEXT NOT NULL,
    profile_id TEXT NOT NULL DEFAULT 'default',
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    egress     TEXT NOT NULL DEFAULT '',
    weight     INTEGER NOT NULL DEFAULT 100,
    role       TEXT NOT NULL DEFAULT 'baseline',
    slot_id    TEXT NOT NULL DEFAULT '',
    pushed_at  BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (node_id, account_id)
);
CREATE INDEX idx_node_accounts_node ON node_accounts(node_id);

-- +goose Down
DROP TABLE node_accounts;
DROP TABLE accounts;
DROP TABLE nodes;
