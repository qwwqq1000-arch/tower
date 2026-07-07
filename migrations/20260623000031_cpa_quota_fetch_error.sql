-- +goose Up
ALTER TABLE cpa_account_quota ADD COLUMN quota_fetch_error TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE cpa_account_quota DROP COLUMN quota_fetch_error;
