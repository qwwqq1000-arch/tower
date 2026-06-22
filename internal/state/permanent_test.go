package state

import "testing"

// cfg with a recoverable threshold of 3 and a permanent threshold of 5.
func permCfg() BreakerCfg {
	return BreakerCfg{PersistStreak: 3, PermStreak: 5, BaseMs: 1000, MaxMs: 10000, Mult: 2}
}

func TestBreaker_PermanentAfterPermStreak(t *testing.T) {
	cfg := permCfg()
	var b Breaker
	// First 3 signals → recoverable open (half-opens after cooldown).
	for i := 0; i < 3; i++ {
		b.OnBanSignal(cfg, 1000)
	}
	if b.Permanent() {
		t.Fatal("should not be permanent at streak 3")
	}
	if b.State(1_000_000) != "half_open" {
		t.Fatalf("recoverable breaker should half_open after cooldown, got %s", b.State(1_000_000))
	}
	// Reach the permanent threshold.
	for i := 3; i < 5; i++ {
		b.OnBanSignal(cfg, 1000)
	}
	if !b.Permanent() {
		t.Fatal("should be permanent at streak 5")
	}
}

func TestBreaker_PermanentNeverHalfOpens(t *testing.T) {
	cfg := permCfg()
	var b Breaker
	for i := 0; i < 5; i++ {
		b.OnBanSignal(cfg, 1000)
	}
	// Far in the future it must STILL be permanent, never half_open.
	if got := b.State(9_999_999_999); got != "permanent" {
		t.Fatalf("permanent breaker must stay permanent, got %s", got)
	}
	if b.TakeTrial(9_999_999_999) {
		t.Fatal("permanent breaker must not yield a half-open trial")
	}
}

func TestAccount_PermanentDeniesDispatch(t *testing.T) {
	cfg := permCfg()
	a := NewAccount(3)
	for i := 0; i < 5; i++ {
		a.Breaker.OnBanSignal(cfg, 1000)
	}
	if a.Status(9_999_999) != "permanent" {
		t.Fatalf("status=%s, want permanent", a.Status(9_999_999))
	}
	ok, trial := a.CanDispatch(9_999_999, "opus", cfg)
	if ok || trial {
		t.Fatalf("permanent account must not dispatch: ok=%v trial=%v", ok, trial)
	}
}

// TestBreaker_PermanentViaTrialFailures reproduces the real dispatch path:
// the breaker opens recoverably at PersistStreak, then repeated failed half-open
// trials must escalate to a permanent ban once the streak reaches PermStreak.
func TestBreaker_PermanentViaTrialFailures(t *testing.T) {
	cfg := permCfg() // PersistStreak 3, PermStreak 5
	var b Breaker
	now := int64(1000)
	// Open recoverably with 3 consecutive ban signals.
	for i := 0; i < 3; i++ {
		b.OnBanSignal(cfg, now)
	}
	if b.Permanent() {
		t.Fatal("should not be permanent yet at streak 3")
	}
	// Drive failing half-open trials; each advances the streak by one.
	for i := 0; i < 10 && !b.Permanent(); i++ {
		// jump past the (growing) cooldown so the breaker is half_open
		now = b.openUntil + 1
		if b.State(now) != "half_open" {
			t.Fatalf("expected half_open at now=%d, got %s", now, b.State(now))
		}
		if !b.TakeTrial(now) {
			t.Fatal("expected to claim a trial")
		}
		b.OnTrialResult(cfg, now, false, true)
	}
	if !b.Permanent() {
		t.Fatal("repeated failed trials must escalate to permanent ban")
	}
	if b.State(now+9_999_999) != "permanent" {
		t.Fatalf("want permanent, got %s", b.State(now+9_999_999))
	}
}

func TestBreaker_RecoverClearsPermanent(t *testing.T) {
	cfg := permCfg()
	var b Breaker
	for i := 0; i < 5; i++ {
		b.OnBanSignal(cfg, 1000)
	}
	if !b.Permanent() {
		t.Fatal("setup: should be permanent")
	}
	b.OnSuccess() // manual recover path
	if b.Permanent() {
		t.Fatal("OnSuccess must clear permanent")
	}
	if b.State(1000) != "closed" {
		t.Fatalf("after recover state=%s, want closed", b.State(1000))
	}
}

func TestBreaker_PermStreakZeroNeverPermanent(t *testing.T) {
	cfg := BreakerCfg{PersistStreak: 3, PermStreak: 0, BaseMs: 1000, MaxMs: 10000, Mult: 2}
	var b Breaker
	for i := 0; i < 50; i++ {
		b.OnBanSignal(cfg, 1000)
	}
	if b.Permanent() {
		t.Fatal("PermStreak=0 must never permanently ban")
	}
}

func TestStore_RecoverAndPermanentRoundTrip(t *testing.T) {
	cfg := permCfg()
	s := NewStore(func() int64 { return 1000 }, func(a, b int64) int64 { return a })
	s.Ensure("n1:default", 3)
	for i := 0; i < 5; i++ {
		s.OnBanSignal("n1:default", cfg)
	}
	if !s.IsPermanent("n1:default") {
		t.Fatal("store should report permanent")
	}
	s.Recover("n1:default")
	if s.IsPermanent("n1:default") {
		t.Fatal("after Recover, must not be permanent")
	}
}
