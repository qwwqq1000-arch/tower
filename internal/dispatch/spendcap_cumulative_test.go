package dispatch

import (
	"context"
	"sync"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

// fixedCap returns a RangeF that always resolves to v (min==max).
func fixedCap(v float64) policy.RangeF { return policy.RangeF{Min: v, Max: v} }

// makeSpendCapSvc returns a minimal Service wired for spend-cap tests.
// capUsd is the fixed cap value; now is the initial clock time.
func makeSpendCapSvc(t *testing.T, capUsd float64, now int64) *Service {
	t.Helper()
	store := state.NewStore(func() int64 { return now }, func(min, max int64) int64 { return min })
	base := policy.Defaults()
	base.SpendCap5hEnabled = true
	base.SpendCap5hUsd = fixedCap(capUsd)
	svc := &Service{Store: store, Base: base, Now: func() int64 { return now }}
	return svc
}

// TestSpendCapFiresOnceAtThreshold verifies that the limit fires when todaySpend reaches T,
// and does NOT fire again on subsequent recordSpend calls while already limited.
func TestSpendCapFiresOnceAtThreshold(t *testing.T) {
	now := int64(86_400_000) // day 1
	svc := makeSpendCapSvc(t, 140, now)
	key := "node1:p1"
	ownerID := "owner1"

	// Seed the policy cache so recordSpend can resolve without a DB.
	ver := svc.policyVer.Load()
	cfg := policy.Defaults()
	cfg.SpendCap5hEnabled = true
	cfg.SpendCap5hUsd = fixedCap(140)
	svc.policyCache.Store(ownerID+"|"+"", cachedPolicyCfg{ver: ver, cfg: cfg})

	// Spend up to just below cap — must NOT be limited.
	svc.recordSpend(context.Background(), ownerID, key, 139.0)
	if svc.Store.IsLimited(key, now) {
		t.Fatal("should not be limited before reaching threshold")
	}

	// Push over cap — must be limited.
	svc.recordSpend(context.Background(), ownerID, key, 2.0) // total 141 >= 140
	if !svc.Store.IsLimited(key, now) {
		t.Fatal("should be limited after exceeding threshold")
	}

	// Record more spend while already limited — still limited (no double-fire panic / state corruption).
	svc.recordSpend(context.Background(), ownerID, key, 10.0)
	if !svc.Store.IsLimited(key, now) {
		t.Fatal("should still be limited after additional spend while limited")
	}
}

// TestSpendCapThresholdRaiseOnRecovery verifies that after the limit expires the
// threshold is raised by nextCap so the next trigger is at T + nextCap.
// Fixed cap = 140 → T₀=140, T₁=280, T₂=420.
//
// This test uses near-instantaneous time advances so all spend stays INSIDE
// the old 5h rolling window. With the OLD code, total spend (145) > T₀ (140) would
// re-fire the limit immediately after recovery. With the NEW code (raised bar at T₁=280),
// 145 does NOT trigger — proving threshold raise is working.
func TestSpendCapThresholdRaiseOnRecovery(t *testing.T) {
	dayMs := int64(86_400_000)
	nowVal := dayMs // day 1
	mu := sync.Mutex{}
	getClock := func() int64 {
		mu.Lock()
		defer mu.Unlock()
		return nowVal
	}
	setClock := func(v int64) {
		mu.Lock()
		defer mu.Unlock()
		nowVal = v
	}

	store := state.NewStore(getClock, func(min, max int64) int64 { return min })
	base := policy.Defaults()
	base.SpendCap5hEnabled = true
	base.SpendCap5hUsd = fixedCap(140)
	svc := &Service{Store: store, Base: base, Now: getClock}

	key := "node1:p1"
	ownerID := "owner1"
	ver := svc.policyVer.Load()
	cfg := policy.Defaults()
	cfg.SpendCap5hEnabled = true
	cfg.SpendCap5hUsd = fixedCap(140)
	svc.policyCache.Store(ownerID+"|"+"", cachedPolicyCfg{ver: ver, cfg: cfg})

	// Round 1: spend to T₀=140 → limited.
	svc.recordSpend(context.Background(), ownerID, key, 140.0)
	if !store.IsLimited(key, getClock()) {
		t.Fatal("round1: expected limited at T=140")
	}

	// Simulate recovery: advance clock by only 1 second (well inside the 5h window)
	// and set limit expiry in the past so IsLimited returns false.
	setClock(nowVal + 1000) // +1s, INSIDE old 5h window
	store.SetLimitedReason(key, svc.Base.MaxConcurrent, nowVal-1, "5h")

	// After recovery: spend 5 more → cumulative=145. OLD code: SpendInWindow=145>140 → re-fires.
	// NEW code: T raised to 280 → 145 < 280 → no limit.
	svc.recordSpend(context.Background(), ownerID, key, 5.0)
	if store.IsLimited(key, getClock()) {
		t.Fatal("round2: after threshold raise to 280, total=145 must NOT trigger limit (distinguishes new vs old model)")
	}

	// Spend enough to hit T₁=280: 140+5+135=280 → limited.
	svc.recordSpend(context.Background(), ownerID, key, 135.0)
	if !store.IsLimited(key, getClock()) {
		t.Fatal("round2: expected limited at T₁=280")
	}

	// Simulate second recovery (+1s).
	setClock(nowVal + 1000)
	store.SetLimitedReason(key, svc.Base.MaxConcurrent, nowVal-1, "5h")

	// Total=280. Spend 5 → total=285. T₂=420 → no limit.
	svc.recordSpend(context.Background(), ownerID, key, 5.0)
	if store.IsLimited(key, getClock()) {
		t.Fatal("round3: total=285 must NOT trigger limit (T₂=420)")
	}

	// Push to T₂=420: 280+5+135=420 → limited.
	svc.recordSpend(context.Background(), ownerID, key, 135.0)
	if !store.IsLimited(key, getClock()) {
		t.Fatal("round3: expected limited at T₂=420")
	}
}

// TestSpendCapDayResetReanchorsThreshold verifies that when a new day begins,
// todaySpend resets to 0 AND the threshold is re-anchored to the initial T₀.
func TestSpendCapDayResetReanchorsThreshold(t *testing.T) {
	dayMs := int64(86_400_000)
	now := dayMs
	store := state.NewStore(func() int64 { return now }, func(min, max int64) int64 { return min })
	base := policy.Defaults()
	base.SpendCap5hEnabled = true
	base.SpendCap5hUsd = fixedCap(140)
	svc := &Service{Store: store, Base: base, Now: func() int64 { return now }}

	key := "node1:p1"
	ownerID := "owner1"
	ver := svc.policyVer.Load()
	cfg := policy.Defaults()
	cfg.SpendCap5hEnabled = true
	cfg.SpendCap5hUsd = fixedCap(140)
	svc.policyCache.Store(ownerID+"|"+"", cachedPolicyCfg{ver: ver, cfg: cfg})

	// Raise threshold to 280 by going through one full cycle.
	svc.recordSpend(context.Background(), ownerID, key, 140.0) // hits T=140 → limited
	store.SetLimitedReason(key, svc.Base.MaxConcurrent, now-1, "5h")
	now2 := now + 18_000_001
	svc.Now = func() int64 { return now2 }
	svc.recordSpend(context.Background(), ownerID, key, 1.0) // triggers raise → T=280, total=141 < 280
	if store.IsLimited(key, now2) {
		t.Fatal("should not be limited at 141 with T=280")
	}

	// Advance to next day.
	now3 := dayMs * 2
	svc.Now = func() int64 { return now3 }
	store.AddSpend(key, 0, now3) // trigger day rollover in store

	// Now spend 140 on day 2 — should trigger because T was re-anchored to 140.
	svc.recordSpend(context.Background(), ownerID, key, 140.0)
	if !store.IsLimited(key, now3) {
		t.Fatal("day2: expected limited at T=140 after re-anchor")
	}
}

// TestSpendCapDisabledIsNoop verifies zero overhead when SpendCap5hEnabled=false.
func TestSpendCapDisabledIsNoop(t *testing.T) {
	now := int64(86_400_000)
	store := state.NewStore(func() int64 { return now }, func(min, max int64) int64 { return min })
	base := policy.Defaults() // SpendCap5hEnabled=false
	svc := &Service{Store: store, Base: base, Now: func() int64 { return now }}

	key := "node1:p1"
	ownerID := "owner1"
	ver := svc.policyVer.Load()
	cfg := policy.Defaults()
	svc.policyCache.Store(ownerID+"|"+"", cachedPolicyCfg{ver: ver, cfg: cfg})

	svc.recordSpend(context.Background(), ownerID, key, 9999.0)
	if store.IsLimited(key, now) {
		t.Fatal("should not be limited when spend cap disabled")
	}
}
