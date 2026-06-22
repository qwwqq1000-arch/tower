-- +goose Up
CREATE TABLE cpa_account_quota (
    account_id                 TEXT PRIMARY KEY,
    five_hour_util             DOUBLE PRECISION NOT NULL DEFAULT 0,
    five_hour_resets_at        TEXT NOT NULL DEFAULT '',
    seven_day_util             DOUBLE PRECISION NOT NULL DEFAULT 0,
    seven_day_resets_at        TEXT NOT NULL DEFAULT '',
    seven_day_sonnet_util      DOUBLE PRECISION NOT NULL DEFAULT 0,
    seven_day_sonnet_resets_at TEXT NOT NULL DEFAULT '',
    updated_at                 BIGINT NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE cpa_account_quota;
