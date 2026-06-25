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
