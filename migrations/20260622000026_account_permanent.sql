-- +goose Up
ALTER TABLE account_state ADD COLUMN permanent BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE account_state DROP COLUMN permanent;
