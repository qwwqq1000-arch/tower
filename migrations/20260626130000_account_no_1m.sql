-- +goose Up
ALTER TABLE accounts ADD COLUMN no_1m_until BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE accounts DROP COLUMN no_1m_until;
