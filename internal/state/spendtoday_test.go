package state

import "testing"

// TestSpendTodayAccumulates verifies that SpendToday sums up costs added on the
// same calendar day (bucket = now/86400000).
func TestSpendTodayAccumulates(t *testing.T) {
	now := int64(86_400_000) // day 1 start (ms)
	clock := func() int64 { return now }
	store := NewStore(clock, func(min, max int64) int64 { return min })

	key := "k1"
	store.AddSpend(key, 10.0, now)
	store.AddSpend(key, 5.5, now+1000) // still same day

	got := store.SpendToday(key, now+1000)
	if got != 15.5 {
		t.Fatalf("expected 15.5, got %f", got)
	}
}

// TestSpendTodayDayRolloverResetsToZero verifies that when now crosses into the next
// day bucket, SpendToday returns 0 (the previous day's total is discarded).
func TestSpendTodayDayRolloverResetsToZero(t *testing.T) {
	dayMs := int64(86_400_000)
	now := dayMs // day 1
	clock := func() int64 { return now }
	store := NewStore(clock, func(min, max int64) int64 { return min })

	key := "k1"
	store.AddSpend(key, 100.0, now) // spend on day 1

	// Advance to day 2
	now2 := dayMs * 2
	got := store.SpendToday(key, now2)
	if got != 0 {
		t.Fatalf("expected 0 after day rollover, got %f", got)
	}
}

// TestSpendTodayAddSpendOnNewDayRestarts verifies that AddSpend on a new day resets
// the accumulator to the new cost only.
func TestSpendTodayAddSpendOnNewDayRestarts(t *testing.T) {
	dayMs := int64(86_400_000)
	now1 := dayMs   // day 1
	now2 := dayMs*2 + 1000 // day 2

	clock := func() int64 { return now1 }
	store := NewStore(clock, func(min, max int64) int64 { return min })

	key := "k1"
	store.AddSpend(key, 80.0, now1) // day 1 spend

	// Add spend on day 2 — should reset accumulator
	store.AddSpend(key, 20.0, now2)

	got := store.SpendToday(key, now2)
	if got != 20.0 {
		t.Fatalf("expected 20.0 on day2 after rollover, got %f", got)
	}
}

// TestSpendTodayUnknownKeyReturnsZero verifies no-panic on an unknown key.
func TestSpendTodayUnknownKeyReturnsZero(t *testing.T) {
	store := NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	if got := store.SpendToday("nonexistent", 100); got != 0 {
		t.Fatalf("expected 0 for unknown key, got %f", got)
	}
}

// TestSeedSpendTodaySetsTotal verifies a warm-restore seed sets the day's total so
// SpendToday returns it on the same calendar day (spend survives restart — without
// this the in-memory total resets to 0 and the daily cap re-grants a full window).
func TestSeedSpendTodaySetsTotal(t *testing.T) {
	now := int64(86_400_000)
	store := NewStore(func() int64 { return now }, func(min, max int64) int64 { return min })

	store.SeedSpendToday("k1", 362.5, now)
	if got := store.SpendToday("k1", now+1000); got != 362.5 {
		t.Fatalf("expected seeded 362.5, got %f", got)
	}
}

// TestSeedSpendTodayThenAddAccumulates verifies post-seed AddSpend adds on top of the
// restored total (not from 0), so the cap counts pre- + post-restart spend together.
func TestSeedSpendTodayThenAddAccumulates(t *testing.T) {
	now := int64(86_400_000)
	store := NewStore(func() int64 { return now }, func(min, max int64) int64 { return min })

	store.SeedSpendToday("k1", 100.0, now)
	store.AddSpend("k1", 25.0, now+1000)
	if got := store.SpendToday("k1", now+1000); got != 125.0 {
		t.Fatalf("expected 125.0 (seed+add), got %f", got)
	}
}

// TestSeedSpendTodayDifferentDayIgnored verifies a seed anchored to an old day is not
// returned once the clock has rolled to a new day (the cap re-anchors on day change).
func TestSeedSpendTodayDifferentDayIgnored(t *testing.T) {
	dayMs := int64(86_400_000)
	store := NewStore(func() int64 { return dayMs }, func(min, max int64) int64 { return min })

	store.SeedSpendToday("k1", 500.0, dayMs) // seeded on day 1
	if got := store.SpendToday("k1", dayMs*2); got != 0 {
		t.Fatalf("expected 0 on day 2 (seed was day 1), got %f", got)
	}
}
