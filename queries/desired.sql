-- name: GetDesiredFeatures :one
SELECT features FROM desired_features WHERE id = 1;

-- name: SetDesiredFeatures :exec
INSERT INTO desired_features (id, features, updated_at) VALUES (1, $1, $2)
ON CONFLICT (id) DO UPDATE SET features = EXCLUDED.features, updated_at = EXCLUDED.updated_at;
