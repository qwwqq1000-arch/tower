-- +goose Up
CREATE TABLE account_spend_threshold (
    key        TEXT PRIMARY KEY,
    threshold  DOUBLE PRECISION NOT NULL,
    day        BIGINT NOT NULL,
    updated_at BIGINT NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE account_spend_threshold;
