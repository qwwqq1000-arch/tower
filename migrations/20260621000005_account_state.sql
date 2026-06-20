-- +goose Up
CREATE TABLE account_state (
    node_id        TEXT NOT NULL,
    profile_id     TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active',
    cooldown_until BIGINT NOT NULL DEFAULT 0,
    ban_streak     INTEGER NOT NULL DEFAULT 0,
    fail_count     INTEGER NOT NULL DEFAULT 0,
    updated_at     BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (node_id, profile_id)
);

-- +goose Down
DROP TABLE account_state;
