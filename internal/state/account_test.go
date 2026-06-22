package state

import "testing"

func TestAccount_StatusPriority(t *testing.T) {
	a := NewAccount(1)
	if a.Status(0) != "active" {
		t.Fatalf("status=%s, want active", a.Status(0))
	}
	c := BreakerCfg{PersistStreak: 1, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	a.Breaker.OnBanSignal(c, 0) // open
	if a.Status(500) != "banned" {
		t.Fatalf("status=%s, want banned", a.Status(500))
	}
	if a.Status(1000) != "half_open" {
		t.Fatalf("status=%s, want half_open", a.Status(1000))
	}
	a.Disabled = true
	if a.Status(1000) != "disabled" {
		t.Fatalf("status=%s, want disabled (highest priority)", a.Status(1000))
	}
}

// A 429-style cooldown during a half-open recovery trial must keep the account in
// 限流·冷却 for the FULL cooldown window — not get masked by a shorter 封控·冷却
// from the breaker reopening. Regression for the "限流冷却没到0就恢复" report.
func TestAccount_CooldownOutlastsHalfOpenBreaker(t *testing.T) {
	a := NewAccount(2)
	c := BreakerCfg{PersistStreak: 1, BaseMs: 10000, MaxMs: 60000, Mult: 2}
	a.Breaker.OnBanSignal(c, 0) // opens until 10000
	if a.Breaker.State(10000) != "half_open" {
		t.Fatalf("setup: want half_open at 10000, got %s", a.Breaker.State(10000))
	}
	a.Breaker.TakeTrial(10000)  // claim recovery trial
	a.Breaker.OnTrialCooldown() // 429 during the trial — must NOT reopen
	a.CoolUntil = 20000         // error-cooldown set by maybeCooldown
	if a.Breaker.State(10000) == "open" {
		t.Fatal("429 during trial must not reopen the breaker")
	}
	if got := a.Status(10000); got != "cooldown" {
		t.Fatalf("status=%s, want cooldown for the full window", got)
	}
	// CanDispatch must stay blocked until the cooldown elapses.
	if ok, _ := a.CanDispatch(15000, "opus", c); ok {
		t.Fatal("must not dispatch while cooling")
	}
	if ok, _ := a.CanDispatch(20001, "opus", c); !ok {
		t.Fatal("must be dispatchable after the cooldown elapses")
	}
}

func TestAccount_CanDispatch_ClosedHappy(t *testing.T) {
	a := NewAccount(1)
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	ok, trial := a.CanDispatch(0, "opus", c)
	if !ok || trial {
		t.Fatalf("closed: ok=%v trial=%v, want true,false", ok, trial)
	}
}

func TestAccount_CanDispatch_DisabledDenied(t *testing.T) {
	a := NewAccount(1)
	a.Disabled = true
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	if ok, _ := a.CanDispatch(0, "opus", c); ok {
		t.Fatal("disabled should deny")
	}
}

func TestAccount_CanDispatch_BannedDenied_HalfOpenTrial(t *testing.T) {
	a := NewAccount(1)
	c := BreakerCfg{PersistStreak: 1, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	a.Breaker.OnBanSignal(c, 0) // open until 1000
	if ok, _ := a.CanDispatch(500, "opus", c); ok {
		t.Fatal("open breaker should deny")
	}
	ok, trial := a.CanDispatch(1000, "opus", c)
	if !ok || !trial {
		t.Fatalf("half_open: ok=%v trial=%v, want true,true", ok, trial)
	}
}

func TestAccount_CanDispatch_NoFreeSlot(t *testing.T) {
	a := NewAccount(1)
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	a.Slots.Acquire(0) // capacity 1 now full
	if ok, _ := a.CanDispatch(0, "opus", c); ok {
		t.Fatal("no free slot should deny")
	}
}

func TestAccount_CanDispatch_ModelLimited(t *testing.T) {
	a := NewAccount(1)
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	a.LimitedUntil = map[string]int64{"opus": 5000}
	if ok, _ := a.CanDispatch(1000, "opus", c); ok {
		t.Fatal("opus limited → deny opus")
	}
	if ok, _ := a.CanDispatch(1000, "sonnet", c); !ok {
		t.Fatal("sonnet not limited → allow")
	}
	if ok, _ := a.CanDispatch(5000, "opus", c); !ok {
		t.Fatal("opus limit expired → allow")
	}
}

func TestAccount_CanDispatch_AllLimited(t *testing.T) {
	a := NewAccount(1)
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	a.LimitedUntil = map[string]int64{"all": 5000}
	if ok, _ := a.CanDispatch(1000, "sonnet", c); ok {
		t.Fatal("all-limited → deny any model")
	}
}

func TestAccount_CanDispatch_WarmupCap(t *testing.T) {
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}

	// WarmupCap=1: one slot in use → deny even though slot capacity is higher.
	a := NewAccount(3) // capacity 3
	a.WarmupCap = 1
	a.Slots.Acquire(0) // 1 in use
	if ok, _ := a.CanDispatch(0, "sonnet", c); ok {
		t.Fatal("WarmupCap=1 with 1 in-use: should deny")
	}

	// WarmupCap=1: no slots in use → allow.
	b := NewAccount(3)
	b.WarmupCap = 1
	if ok, _ := b.CanDispatch(0, "sonnet", c); !ok {
		t.Fatal("WarmupCap=1 with 0 in-use: should allow")
	}

	// WarmupCap=0 (disabled): normal slot rules apply.
	d := NewAccount(3)
	d.WarmupCap = 0
	d.Slots.Acquire(0)
	d.Slots.Acquire(0)
	if ok, _ := d.CanDispatch(0, "sonnet", c); !ok {
		t.Fatal("WarmupCap=0 with 2/3 in-use: should allow (no warmup limit)")
	}
	d.Slots.Acquire(0)
	if ok, _ := d.CanDispatch(0, "sonnet", c); ok {
		t.Fatal("WarmupCap=0 with 3/3 in-use: capacity exhausted → deny")
	}
}
