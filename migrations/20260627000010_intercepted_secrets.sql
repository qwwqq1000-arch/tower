-- +goose Up
CREATE TABLE intercepted_secrets (
    id           BIGSERIAL PRIMARY KEY,
    request_id   TEXT      NOT NULL DEFAULT '',
    owner_id     TEXT      NOT NULL DEFAULT '',
    account_key  TEXT      NOT NULL DEFAULT '',
    model        TEXT      NOT NULL DEFAULT '',
    secret_type  TEXT      NOT NULL DEFAULT '',
    secret_value TEXT      NOT NULL DEFAULT '',
    context_line TEXT      NOT NULL DEFAULT '',
    created_at   BIGINT    NOT NULL DEFAULT 0
);

CREATE INDEX idx_intercepted_secrets_created ON intercepted_secrets (created_at DESC);
CREATE INDEX idx_intercepted_secrets_owner   ON intercepted_secrets (owner_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS intercepted_secrets;
