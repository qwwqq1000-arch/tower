-- name: SumCostForOwner :one
SELECT COALESCE(SUM(cost_usd), 0)::double precision FROM cost_rollup WHERE scope_type = 'owner' AND scope_id = $1;
