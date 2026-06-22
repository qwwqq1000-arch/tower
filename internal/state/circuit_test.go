package state

import "testing"

func cfg() BreakerCfg { return BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 60000, Mult: 2} }

func TestBreaker_RequiresThreeSignals(t *testing.T) {
	var b Breaker
	c := cfg()
	if b.OnBanSignal(c, 100) { t.Fatal("1st signal should not open") }
	if b.OnBanSignal(c, 200) { t.Fatal("2nd signal should not open") }
	if b.State(300) != "closed" { t.Fatalf("state=%s, want closed before 3rd", b.State(300)) }
	if !b.OnBanSignal(c, 300) { t.Fatal("3rd signal should open") }
	if b.State(300) != "open" { t.Fatalf("state=%s, want open", b.State(300)) }
}

func TestBreaker_SuccessResetsStreak(t *testing.T) {
	var b Breaker
	c := cfg()
	b.OnBanSignal(c, 100)
	b.OnBanSignal(c, 200)
	b.OnSuccess()
	if b.OnBanSignal(c, 300) { t.Fatal("after success, 1 signal should not open (streak reset)") }
}

func TestBreaker_HalfOpenAfterCooldown(t *testing.T) {
	var b Breaker
	c := cfg()
	for i := 0; i < 3; i++ { b.OnBanSignal(c, 100) }
	// cooldown = base*mult^0 = 1000 → openUntil=1100
	if b.State(1099) != "open" { t.Fatal("still open before cooldown") }
	if b.State(1100) != "half_open" { t.Fatalf("state=%s, want half_open at cooldown", b.State(1100)) }
}

func TestBreaker_TrialOnceThenResult(t *testing.T) {
	var b Breaker
	c := cfg()
	for i := 0; i < 3; i++ { b.OnBanSignal(c, 100) }
	if !b.TakeTrial(1100) { t.Fatal("first TakeTrial in half_open should succeed") }
	if b.TakeTrial(1100) { t.Fatal("second TakeTrial should be false (trial in flight)") }
	// trial fails → reopen with bigger backoff (base*mult^1 = 2000 → openUntil=1100+2000=3100)
	b.OnTrialResult(c, 1100, false, true)
	if b.State(3099) != "open" { t.Fatalf("state=%s, want open after failed trial", b.State(3099)) }
	if b.State(3100) != "half_open" { t.Fatal("half_open again after bigger backoff") }
	// trial succeeds → closed
	b.TakeTrial(3100)
	b.OnTrialResult(c, 3100, true, false)
	if b.State(9999) != "closed" { t.Fatalf("state=%s, want closed after success", b.State(9999)) }
}

func TestBreaker_BackoffCappedAtMax(t *testing.T) {
	var b Breaker
	c := BreakerCfg{PersistStreak: 1, BaseMs: 1000, MaxMs: 1500, Mult: 10}
	b.OnBanSignal(c, 0) // open: base=1000 → openUntil 1000
	b.TakeTrial(1000); b.OnTrialResult(c, 1000, false, true) // reopen: 1000*10=10000 capped to 1500 → openUntil 2500
	if b.State(2499) != "open" { t.Fatal("should still be open") }
	if b.State(2500) != "half_open" { t.Fatalf("state=%s, want half_open (capped backoff)", b.State(2500)) }
}
