-- +goose Up
CREATE TABLE tenants (
    id             TEXT PRIMARY KEY,
    username       TEXT NOT NULL UNIQUE,
    pw_hash        TEXT NOT NULL,
    salt           TEXT NOT NULL,
    role           TEXT NOT NULL DEFAULT 'tenant',
    ingest_key     TEXT NOT NULL UNIQUE,
    must_change_pw BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE tenants;
