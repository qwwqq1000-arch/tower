// Package aging assigns fresh accounts from the yanghao holding pool to internal
// employees so they age (organic usage), then graduates aged accounts to the
// test pool after the configured window. aged_by is retained for traceability.
package aging

import (
	"context"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

const dayMs = int64(24 * 60 * 60 * 1000)

type Ager struct {
	Q   *sqlc.Queries
	Now func() int64
}

func (a *Ager) now() int64 {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now().UnixMilli()
}

// RunOnce graduates due accounts and tops up each internal employee.
func (a *Ager) RunOnce(ctx context.Context) error {
	cfg, err := a.Q.GetAgingConfig(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}
	src, err := a.Q.GetTenantByUsername(ctx, "yanghao")
	if err != nil {
		return err // no holding pool → nothing to do
	}
	dst, err := a.Q.GetTenantByUsername(ctx, "test")
	if err != nil {
		return err
	}
	now := a.now()

	// 1) Graduate accounts aged past the window → move to test (keep aged_by).
	cutoff := now - int64(cfg.AgingDays)*dayMs
	if due, derr := a.Q.DueForGraduationIDs(ctx, cutoff); derr == nil {
		for _, id := range due {
			_ = a.Q.GraduateAccount(ctx, id, dst.ID)
		}
	}

	// 2) Top up each internal employee to accounts_per_employee from yanghao.
	emps, eerr := a.Q.ListInternalTenants(ctx)
	if eerr != nil {
		return eerr
	}
	for _, e := range emps {
		have, cerr := a.Q.CountOwnerAccounts(ctx, e.ID)
		if cerr != nil {
			continue
		}
		need := cfg.AccountsPerEmployee - have
		if need <= 0 {
			continue
		}
		ids, terr := a.Q.TakeFreshAccountIDs(ctx, src.ID, need)
		if terr != nil {
			continue
		}
		for _, id := range ids {
			_ = a.Q.AssignAccountForAging(ctx, id, e.ID, e.Username, now)
		}
	}
	return nil
}

func (a *Ager) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	_ = a.RunOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = a.RunOnce(ctx)
		}
	}
}
