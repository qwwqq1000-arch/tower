package state

import "testing"

func TestAccount_StatusPriority(t *testing.T) {
	a := NewAccount(1)
	if a.Status(0) != "active" { t.Fatalf("status=%s, want active", a.Status(0)) }
	c := BreakerCfg{PersistStreak: 1, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	a.Breaker.OnBanSignal(c, 0) // open
	if a.Status(500) != "banned" { t.Fatalf("status=%s, want banned", a.Status(500)) }
	if a.Status(1000) != "half_open" { t.Fatalf("status=%s, want half_open", a.Status(1000)) }
	a.Disabled = true
	if a.Status(1000) != "disabled" { t.Fatalf("status=%s, want disabled (highest priority)", a.Status(1000)) }
}

func TestAccount_CanDispatch_ClosedHappy(t *testing.T) {
	a := NewAccount(1)
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	ok, trial := a.CanDispatch(0, "opus", c)
	if !ok || trial { t.Fatalf("closed: ok=%v trial=%v, want true,false", ok, trial) }
}

func TestAccount_CanDispatch_DisabledDenied(t *testing.T) {
	a := NewAccount(1)
	a.Disabled = true
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	if ok, _ := a.CanDispatch(0, "opus", c); ok { t.Fatal("disabled should deny") }
}

func TestAccount_CanDispatch_BannedDenied_HalfOpenTrial(t *testing.T) {
	a := NewAccount(1)
	c := BreakerCfg{PersistStreak: 1, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	a.Breaker.OnBanSignal(c, 0) // open until 1000
	if ok, _ := a.CanDispatch(500, "opus", c); ok { t.Fatal("open breaker should deny") }
	ok, trial := a.CanDispatch(1000, "opus", c)
	if !ok || !trial { t.Fatalf("half_open: ok=%v trial=%v, want true,true", ok, trial) }
}

func TestAccount_CanDispatch_NoFreeSlot(t *testing.T) {
	a := NewAccount(1)
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	a.Slots.Acquire(0) // capacity 1 now full
	if ok, _ := a.CanDispatch(0, "opus", c); ok { t.Fatal("no free slot should deny") }
}

func TestAccount_CanDispatch_ModelLimited(t *testing.T) {
	a := NewAccount(1)
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	a.LimitedUntil = map[string]int64{"opus": 5000}
	if ok, _ := a.CanDispatch(1000, "opus", c); ok { t.Fatal("opus limited → deny opus") }
	if ok, _ := a.CanDispatch(1000, "sonnet", c); !ok { t.Fatal("sonnet not limited → allow") }
	if ok, _ := a.CanDispatch(5000, "opus", c); !ok { t.Fatal("opus limit expired → allow") }
}

func TestAccount_CanDispatch_AllLimited(t *testing.T) {
	a := NewAccount(1)
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	a.LimitedUntil = map[string]int64{"all": 5000}
	if ok, _ := a.CanDispatch(1000, "sonnet", c); ok { t.Fatal("all-limited → deny any model") }
}
