-- name: CreateSlot :one
INSERT INTO slots (id, name, start_min, end_min)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListSlots :many
SELECT * FROM slots ORDER BY name;

-- name: DeleteSlot :exec
DELETE FROM slots WHERE id = $1;

-- name: SetSlotEnabled :exec
UPDATE slots SET enabled = $2 WHERE id = $1;
