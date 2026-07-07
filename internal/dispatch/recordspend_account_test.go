package dispatch

import (
	"context"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

// TestRecordSpendResolvesPerAccount verifies that recordSpend looks up the
// business accountID for a dispatch key (via the keyAccount map populated in
// buildCandidates) and resolves the per-account spend-cap config, rather than
// always resolving with accountID="". This is DB-free: resolveConfig returns the
// pre-seeded policyCache entry on a version match before touching the DB.
func TestRecordSpendResolvesPerAccount(t *testing.T) {
	now := int64(1_000_000_000)
	store := state.NewStore(func() int64 { return now }, func(min, max int64) int64 { return min })
	svc := &Service{Store: store, Base: policy.Defaults(), Now: func() int64 { return now }}

	ownerID := "owner1"
	accountID := "acc_42"
	key := "node1:profile1"
	// Map the dispatch key to the business account (as buildCandidates would).
	svc.keyAccount.Store(key, accountID)

	// Seed the policy cache for THIS account at the current version with a config
	// whose 5h spend cap is enabled and tiny, so any spend trips it. The "" entry
	// is left absent: if recordSpend wrongly resolved with "", the DB-less path
	// would panic on s.Q.ListPolicies and be swallowed by recover() — no limit set.
	ver := svc.policyVer.Load()
	acctCfg := policy.Defaults()
	acctCfg.SpendCap5hEnabled = true
	acctCfg.SpendCap5hUsd = policy.RangeF{Min: 1, Max: 1} // cap = $1
	svc.policyCache.Store(ownerID+"|"+accountID, cachedPolicyCfg{ver: ver, cfg: acctCfg})

	// Spend over the per-account cap → recordSpend must SetLimited via the
	// per-account config (proving it resolved with accountID, not "").
	svc.recordSpend(context.Background(), ownerID, key, 5.0)

	snap := store.Snapshot(now)
	limited := false
	for _, sn := range snap {
		if sn.Key == key && sn.Limited {
			limited = true
		}
	}
	if !limited {
		t.Fatalf("expected key %q to be SetLimited via per-account spend cap; recordSpend did not resolve with the account key", key)
	}
}

// TestRecordSpendFallsBackToEmptyAccount verifies behavior-neutrality: when a key
// has no keyAccount mapping, recordSpend resolves with accountID="" (the seeded
// owner-level entry), matching pre-change behavior.
func TestRecordSpendFallsBackToEmptyAccount(t *testing.T) {
	now := int64(1_000_000_000)
	store := state.NewStore(func() int64 { return now }, func(min, max int64) int64 { return min })
	svc := &Service{Store: store, Base: policy.Defaults(), Now: func() int64 { return now }}

	ownerID := "owner1"
	key := "node9:profile9" // no keyAccount mapping → accountID resolves to ""

	// Seed the "" entry with both caps disabled → recordSpend is a no-op.
	ver := svc.policyVer.Load()
	noCap := policy.Defaults() // SpendCap5h/7dEnabled default false
	svc.policyCache.Store(ownerID+"|"+"", cachedPolicyCfg{ver: ver, cfg: noCap})

	svc.recordSpend(context.Background(), ownerID, key, 9999.0)

	snap := store.Snapshot(now)
	for _, sn := range snap {
		if sn.Key == key && sn.Limited {
			t.Fatalf("expected no limit when caps disabled and no account mapping, but key %q was limited", key)
		}
	}
}
