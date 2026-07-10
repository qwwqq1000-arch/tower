package sqlc

import "context"

// AgingConfig holds the internal-employee account-aging settings (single row).
type AgingConfig struct {
	AccountsPerEmployee int
	AgingDays           int
	Enabled             bool
}

func (q *Queries) GetAgingConfig(ctx context.Context) (AgingConfig, error) {
	var c AgingConfig
	err := q.db.QueryRow(ctx, `SELECT accounts_per_employee, aging_days, enabled FROM internal_aging_config WHERE id=1`).
		Scan(&c.AccountsPerEmployee, &c.AgingDays, &c.Enabled)
	return c, err
}

func (q *Queries) SetAgingConfig(ctx context.Context, perEmployee, agingDays int, enabled bool) error {
	_, err := q.db.Exec(ctx, `INSERT INTO internal_aging_config (id, accounts_per_employee, aging_days, enabled)
		VALUES (1,$1,$2,$3) ON CONFLICT (id) DO UPDATE SET accounts_per_employee=$1, aging_days=$2, enabled=$3`,
		perEmployee, agingDays, enabled)
	return err
}

func (q *Queries) SetTenantInternal(ctx context.Context, id string, internal bool) error {
	_, err := q.db.Exec(ctx, `UPDATE tenants SET is_internal=$2 WHERE id=$1`, id, internal)
	return err
}

type InternalTenant struct {
	ID       string
	Username string
}

func (q *Queries) ListInternalTenants(ctx context.Context) ([]InternalTenant, error) {
	rows, err := q.db.Query(ctx, `SELECT id, username FROM tenants WHERE is_internal=true ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InternalTenant
	for rows.Next() {
		var t InternalTenant
		if err := rows.Scan(&t.ID, &t.Username); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (q *Queries) CountOwnerAccounts(ctx context.Context, ownerID string) (int, error) {
	var n int
	err := q.db.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE owner_id=$1`, ownerID).Scan(&n)
	return n, err
}

// TakeFreshAccountIDs pulls up to limit agable accounts from a source pool:
// owned by srcOwner, not banned, currently assigned to an enabled node.
// Oldest-onboarded first so the freshest inventory ages soonest.
func (q *Queries) TakeFreshAccountIDs(ctx context.Context, srcOwner string, limit int) ([]string, error) {
	rows, err := q.db.Query(ctx, `
		SELECT a.id FROM accounts a
		WHERE a.owner_id=$1
		  AND COALESCE(a.status,'') <> 'banned'
		  AND EXISTS (SELECT 1 FROM node_accounts na WHERE na.account_id=a.id AND na.enabled)
		ORDER BY a.onboarded_at ASC
		LIMIT $2`, srcOwner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (q *Queries) AssignAccountForAging(ctx context.Context, accountID, employeeID, employeeName string, startedAt int64) error {
	_, err := q.db.Exec(ctx, `UPDATE accounts SET owner_id=$2, aged_by=$3, aging_started_at=$4 WHERE id=$1`,
		accountID, employeeID, employeeName, startedAt)
	return err
}

// DueForGraduationIDs lists accounts owned by internal employees whose aging
// window elapsed (aging_started_at in 1..cutoff).
func (q *Queries) DueForGraduationIDs(ctx context.Context, cutoff int64) ([]string, error) {
	rows, err := q.db.Query(ctx, `
		SELECT a.id FROM accounts a
		JOIN tenants t ON t.id=a.owner_id
		WHERE t.is_internal=true AND a.aging_started_at>0 AND a.aging_started_at<=$1`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GraduateAccount moves an aged account to destOwner (test), clearing the aging
// clock but KEEPING aged_by for traceability.
func (q *Queries) GraduateAccount(ctx context.Context, accountID, destOwner string) error {
	_, err := q.db.Exec(ctx, `UPDATE accounts SET owner_id=$2, aging_started_at=0 WHERE id=$1`, accountID, destOwner)
	return err
}

// ListAgedBy returns accountID -> the internal employee username that aged it
// (retained after graduation to test, for traceability in the account pool).
func (q *Queries) ListAgedBy(ctx context.Context) (map[string]string, error) {
	rows, err := q.db.Query(ctx, `SELECT id, aged_by FROM accounts WHERE aged_by <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var id, ab string
		if err := rows.Scan(&id, &ab); err != nil {
			return nil, err
		}
		m[id] = ab
	}
	return m, rows.Err()
}
