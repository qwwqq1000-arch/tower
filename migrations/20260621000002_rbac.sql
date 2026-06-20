-- +goose Up
CREATE TABLE roles (
    name        TEXT PRIMARY KEY,
    builtin     BOOLEAN NOT NULL DEFAULT FALSE,
    permissions JSONB NOT NULL DEFAULT '[]'
);

CREATE TABLE node_groups (
    id       TEXT PRIMARY KEY,
    name     TEXT NOT NULL,
    owner_id TEXT NOT NULL DEFAULT ''
);

CREATE TABLE group_members (
    user_id  TEXT NOT NULL,
    group_id TEXT NOT NULL,
    role     TEXT NOT NULL,
    PRIMARY KEY (user_id, group_id)
);

INSERT INTO roles (name, builtin, permissions) VALUES
  ('superadmin', TRUE, '["*"]'),
  ('admin', TRUE, '["nodes:read","nodes:write","nodes:provision","nodes:delete","accounts:read","accounts:onboard","accounts:write","accounts:migrate","accounts:delete","policies:read","policies:write","dispatch_keys:manage","fallback:manage","dispatch:read","logs:read","billing:read","billing:settle","audit:read","settings:write"]'),
  ('operator', TRUE, '["nodes:read","nodes:write","nodes:provision","accounts:read","accounts:onboard","accounts:write","accounts:migrate","policies:read","dispatch:read","logs:read"]'),
  ('tenant', TRUE, '["nodes:read","accounts:read","policies:read","dispatch_keys:manage","fallback:manage","dispatch:read","logs:read","billing:read"]'),
  ('viewer', TRUE, '["nodes:read","accounts:read","dispatch:read","logs:read","billing:read"]');

-- +goose Down
DROP TABLE group_members;
DROP TABLE node_groups;
DROP TABLE roles;
