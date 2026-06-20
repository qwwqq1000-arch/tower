-- +goose Up
CREATE TABLE cost_rollup (
    scope_type TEXT NOT NULL,
    scope_id   TEXT NOT NULL,
    day        TEXT NOT NULL,
    model      TEXT NOT NULL,
    requests   BIGINT NOT NULL DEFAULT 0,
    tokens_in  BIGINT NOT NULL DEFAULT 0,
    tokens_out BIGINT NOT NULL DEFAULT 0,
    cost_usd   DOUBLE PRECISION NOT NULL DEFAULT 0,
    PRIMARY KEY (scope_type, scope_id, day, model)
);

CREATE TABLE billing_ledger (
    id            BIGSERIAL PRIMARY KEY,
    tenant_id     TEXT NOT NULL DEFAULT '',
    ts            BIGINT NOT NULL DEFAULT 0,
    type          TEXT NOT NULL DEFAULT '',
    amount_usd    DOUBLE PRECISION NOT NULL DEFAULT 0,
    balance_after DOUBLE PRECISION NOT NULL DEFAULT 0,
    ref           TEXT NOT NULL DEFAULT '',
    note          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_billing_ledger_tenant ON billing_ledger(tenant_id, ts);

CREATE TABLE settlements (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL DEFAULT '',
    period_start BIGINT NOT NULL DEFAULT 0,
    period_end   BIGINT NOT NULL DEFAULT 0,
    gross_usd    DOUBLE PRECISION NOT NULL DEFAULT 0,
    settled_usd  DOUBLE PRECISION NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'pending',
    created_at   BIGINT NOT NULL DEFAULT 0,
    settled_at   BIGINT NOT NULL DEFAULT 0,
    note         TEXT NOT NULL DEFAULT ''
);

CREATE TABLE hosting_rates (
    tenant_id      TEXT NOT NULL,
    rate           DOUBLE PRECISION NOT NULL DEFAULT 0,
    effective_from BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, effective_from)
);

CREATE TABLE fallback_spend (
    channel_id       TEXT NOT NULL,
    day              TEXT NOT NULL,
    requests         BIGINT NOT NULL DEFAULT 0,
    est_cost_usd     DOUBLE PRECISION NOT NULL DEFAULT 0,
    balance_observed DOUBLE PRECISION NOT NULL DEFAULT 0,
    PRIMARY KEY (channel_id, day)
);

-- +goose Down
DROP TABLE fallback_spend;
DROP TABLE hosting_rates;
DROP TABLE settlements;
DROP TABLE billing_ledger;
DROP TABLE cost_rollup;
