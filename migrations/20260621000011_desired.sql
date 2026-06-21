-- +goose Up
CREATE TABLE desired_features (
    id         INTEGER PRIMARY KEY DEFAULT 1,
    features   JSONB NOT NULL DEFAULT '{}',
    updated_at BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT desired_features_singleton CHECK (id = 1)
);

-- +goose Down
DROP TABLE desired_features;
