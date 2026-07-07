package state

import "testing"

func TestStore_Snapshot(t *testing.T) {
	s := NewStore(func() int64 { return 0 }, func(a, b int64) int64 { return a })
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1, MaxMs: 1, Mult: 2}
	s.Ensure("b:p", 2)
	s.Ensure("a:p", 2)
	s.TryDispatch("a:p", "opus", c) // a has 1 inflight
	snap := s.Snapshot(0)
	if len(snap) != 2 {
		t.Fatalf("snapshot len=%d", len(snap))
	}
	if snap[0].Key != "a:p" || snap[1].Key != "b:p" {
		t.Fatalf("not sorted by key: %+v", snap)
	}
	if snap[0].Inflight != 1 || snap[0].Available != 1 {
		t.Fatalf("a:p inflight/avail = %d/%d", snap[0].Inflight, snap[0].Available)
	}
	if snap[0].Status != "active" {
		t.Fatalf("a:p status=%s", snap[0].Status)
	}
}

func TestStore_Snapshot_Limited(t *testing.T) {
	// A quota-saturated account is rotated out via SetLimited while its breaker
	// stays "active". Snapshot must surface Limited=true + the reset deadline so the
	// UI shows 限额 instead of a misleading 活跃 (quota-3).
	s := NewStore(func() int64 { return 1000 }, func(a, b int64) int64 { return a })
	s.Ensure("n:acct", 5)
	s.SetLimited("n:acct", 5, map[string]int64{"all": 9000}) // limited until 9000 (> now 1000)
	snap := s.Snapshot(1000)
	if len(snap) != 1 {
		t.Fatalf("snapshot len=%d", len(snap))
	}
	if snap[0].Status != "active" {
		t.Fatalf("breaker status should still be active, got %s", snap[0].Status)
	}
	if !snap[0].Limited || snap[0].LimitedUntil != 9000 {
		t.Fatalf("expected Limited=true until=9000, got Limited=%v until=%d", snap[0].Limited, snap[0].LimitedUntil)
	}
	// After the deadline passes, the account is no longer limited.
	if expired := s.Snapshot(9000); expired[0].Limited {
		t.Fatalf("expired quota-limit should not report Limited")
	}
}
