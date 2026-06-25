-- +goose Up
CREATE TABLE account_limit_state (
    key           TEXT PRIMARY KEY,
    limited_until BIGINT NOT NULL,
    limit_reason  TEXT NOT NULL DEFAULT '',
    updated_at    BIGINT NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE account_limit_state;
