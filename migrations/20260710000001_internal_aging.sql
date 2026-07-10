-- +goose Up
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS is_internal boolean NOT NULL DEFAULT false;
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS aged_by text NOT NULL DEFAULT '';
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS aging_started_at bigint NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS internal_aging_config (
  id integer PRIMARY KEY,
  accounts_per_employee integer NOT NULL DEFAULT 1,
  aging_days integer NOT NULL DEFAULT 7,
  enabled boolean NOT NULL DEFAULT true
);
INSERT INTO internal_aging_config (id, accounts_per_employee, aging_days, enabled)
  VALUES (1, 1, 7, true) ON CONFLICT (id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS internal_aging_config;
ALTER TABLE accounts DROP COLUMN IF EXISTS aging_started_at;
ALTER TABLE accounts DROP COLUMN IF EXISTS aged_by;
ALTER TABLE tenants DROP COLUMN IF EXISTS is_internal;
