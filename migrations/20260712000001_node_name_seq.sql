-- +goose Up
CREATE SEQUENCE IF NOT EXISTS node_name_seq START WITH 1200;

-- +goose Down
DROP SEQUENCE IF EXISTS node_name_seq;
