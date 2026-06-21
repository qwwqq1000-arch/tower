-- +goose Up
CREATE TABLE slots (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL DEFAULT '',
  start_min  INTEGER NOT NULL DEFAULT 0,   -- minute-of-day [0,1440), Beijing
  end_min    INTEGER NOT NULL DEFAULT 1440,
  enabled    BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose Down
DROP TABLE slots;
