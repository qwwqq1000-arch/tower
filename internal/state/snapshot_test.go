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
