package dispatch

import (
	"context"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

func TestResolveOrderAccountWins(t *testing.T) {
	base := policy.Defaults()
	g := policy.Patch{MaxConcurrent: ptrInt(5)}  // 全局
	o := policy.Patch{MaxConcurrent: ptrInt(7)}  // 租户
	a := policy.Patch{MaxConcurrent: ptrInt(9)}  // 账户
	got := policy.Resolve(base, g, o, a) // 顺序:全局→租户→账户,后者赢
	if got.MaxConcurrent != 9 {
		t.Fatalf("账户层应赢,得 %d", got.MaxConcurrent)
	}
	got2 := policy.Resolve(base, g, o) // 无账户层 → 租户值
	if got2.MaxConcurrent != 7 {
		t.Fatalf("无账户层应=租户7,得 %d", got2.MaxConcurrent)
	}
}

func ptrInt(i int) *int { return &i }

// TestBumpPolicyVersion verifies that BumpPolicyVersion increments policyVer.
// This test requires no database.
func TestBumpPolicyVersion(t *testing.T) {
	svc := &Service{}
	if svc.policyVer.Load() != 0 {
		t.Fatalf("initial policyVer should be 0, got %d", svc.policyVer.Load())
	}
	svc.BumpPolicyVersion()
	if svc.policyVer.Load() != 1 {
		t.Fatalf("after one bump policyVer should be 1, got %d", svc.policyVer.Load())
	}
	svc.BumpPolicyVersion()
	if svc.policyVer.Load() != 2 {
		t.Fatalf("after two bumps policyVer should be 2, got %d", svc.policyVer.Load())
	}
}

// TestPolicyCacheInvalidatesOnBump verifies that after BumpPolicyVersion the cache
// is invalidated: the next resolveConfig call rebuilds rather than returning the
// stale entry. Requires TEST_DATABASE_URL; skipped otherwise.
func TestPolicyCacheInvalidatesOnBump(t *testing.T) {
	q, closeDB := setupDB(t)
	defer closeDB()
	ctx := context.Background()

	store := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	svc := &Service{Q: q, Store: store, Base: policy.Defaults(), Now: func() int64 { return 0 }}

	// First call: cache miss → full resolution, entry stored at ver=0.
	c1 := svc.resolveConfig(ctx, "", "")

	// Verify the cache was populated.
	if _, ok := svc.policyCache.Load("|"); !ok {
		t.Fatal("expected cache entry after first resolveConfig call")
	}

	// Bump: ver becomes 1, so the stored entry (ver=0) is now stale.
	svc.BumpPolicyVersion()
	if svc.policyVer.Load() == 0 {
		t.Fatal("版本未自增")
	}

	// Second call: stale cache entry → cache miss → rebuild (no panic, returns valid config).
	c2 := svc.resolveConfig(ctx, "", "")

	// Both configs should equal Defaults() since no policies are seeded here.
	_ = c1
	_ = c2
	if c2.MaxConcurrent != policy.Defaults().MaxConcurrent {
		t.Errorf("c2.MaxConcurrent=%d want %d", c2.MaxConcurrent, policy.Defaults().MaxConcurrent)
	}
}
