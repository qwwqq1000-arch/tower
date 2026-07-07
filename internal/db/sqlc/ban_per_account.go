package sqlc

import "context"

// BanCountsPerAccountRow holds the per-email ban count.
type BanCountsPerAccountRow struct {
	Email string `json:"email"`
	N     int64  `json:"n"`
}

const banCountsPerAccount = `
SELECT COALESCE(a.email, be.profile_id) AS email, count(*) AS n
FROM ban_episodes be
JOIN node_accounts na ON na.node_id = be.node_id AND na.profile_id = be.profile_id
JOIN accounts a ON a.id = na.account_id
GROUP BY a.email, be.profile_id
ORDER BY n DESC
LIMIT 200
`

// BanCountsPerAccount returns per-email (or profile_id) ban episode counts, global scope.
func (q *Queries) BanCountsPerAccount(ctx context.Context) ([]BanCountsPerAccountRow, error) {
	rows, err := q.db.Query(ctx, banCountsPerAccount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []BanCountsPerAccountRow
	for rows.Next() {
		var i BanCountsPerAccountRow
		if err := rows.Scan(&i.Email, &i.N); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

const banCountsPerAccountForOwner = `
SELECT COALESCE(a.email, be.profile_id) AS email, count(*) AS n
FROM ban_episodes be
JOIN node_accounts na ON na.node_id = be.node_id AND na.profile_id = be.profile_id
JOIN accounts a ON a.id = na.account_id
WHERE a.owner_id = $1
GROUP BY a.email, be.profile_id
ORDER BY n DESC
LIMIT 200
`

// BanCountsPerAccountForOwner returns per-email ban counts scoped to an owner.
func (q *Queries) BanCountsPerAccountForOwner(ctx context.Context, ownerID string) ([]BanCountsPerAccountRow, error) {
	rows, err := q.db.Query(ctx, banCountsPerAccountForOwner, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []BanCountsPerAccountRow
	for rows.Next() {
		var i BanCountsPerAccountRow
		if err := rows.Scan(&i.Email, &i.N); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}
