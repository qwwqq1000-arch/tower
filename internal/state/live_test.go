package state

import "testing"

func TestAccount_OfflineDenies(t *testing.T) {
	a := NewAccount(1)
	a.Offline = true
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1, MaxMs: 1, Mult: 2}
	if ok, _ := a.CanDispatch(0, "opus", c); ok {
		t.Fatal("offline account must not dispatch")
	}
	if a.Status(0) != "offline" {
		t.Fatalf("status=%s, want offline", a.Status(0))
	}
	a.Disabled = true
	if a.Status(0) != "disabled" {
		t.Fatalf("disabled outranks offline, status=%s", a.Status(0))
	}
}

func TestStore_SetLiveState(t *testing.T) {
	s := NewStore(func() int64 { return 1000 }, func(a, b int64) int64 { return a })
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1, MaxMs: 1, Mult: 2}

	s.SetLimited("k", 2, map[string]int64{"opus": 5000})
	if s.TryDispatch("k", "opus", c) {
		t.Fatal("opus limited → no dispatch")
	}
	if !s.TryDispatch("k", "sonnet", c) {
		t.Fatal("sonnet not limited → dispatch ok")
	}

	s.SetOffline("k", 2, true)
	if s.TryDispatch("k", "sonnet", c) {
		t.Fatal("offline → no dispatch")
	}
	s.SetOffline("k", 2, false)
	if !s.TryDispatch("k", "sonnet", c) {
		t.Fatal("back online → dispatch ok")
	}

	s.SetDisabled("k", 2, true)
	if s.TryDispatch("k", "sonnet", c) {
		t.Fatal("disabled → no dispatch")
	}
}
