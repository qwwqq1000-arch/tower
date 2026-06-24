package dispatch

import (
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// TestWeightedOrderWithinPriority_SamePriority verifies that channels at the
// same priority tier are ordered proportionally to their weight over many runs.
func TestWeightedOrderWithinPriority_SamePriority(t *testing.T) {
	a := sqlc.FallbackChannel{ID: "A", Priority: 1, Weight: 3}
	b := sqlc.FallbackChannel{ID: "B", Priority: 1, Weight: 1}
	channels := []sqlc.FallbackChannel{a, b}

	const N = 1000
	aFirst := 0
	for i := 0; i < N; i++ {
		result := weightedOrderWithinPriority(channels)
		if len(result) != 2 {
			t.Fatalf("expected 2 channels, got %d", len(result))
		}
		if result[0].ID == "A" {
			aFirst++
		}
	}

	pct := float64(aFirst) / float64(N)
	// A has weight 3, B has weight 1: A should be first ~75% of the time.
	// We allow 60-90% for statistical variation.
	if pct < 0.60 || pct > 0.90 {
		t.Errorf("channel A (weight=3) was first %.1f%% of the time; expected 60-90%%", pct*100)
	}
}

// TestWeightedOrderWithinPriority_DifferentPriorities verifies that priority
// tiers are kept in ascending order regardless of channel weights.
func TestWeightedOrderWithinPriority_DifferentPriorities(t *testing.T) {
	// Lower number = higher priority = comes first.
	lo := sqlc.FallbackChannel{ID: "LO", Priority: 1, Weight: 1}  // high priority, low weight
	hi := sqlc.FallbackChannel{ID: "HI", Priority: 2, Weight: 99} // low priority, high weight

	// Input is already sorted by priority (as ListEnabledFallbackChannels returns).
	channels := []sqlc.FallbackChannel{lo, hi}

	for i := 0; i < 100; i++ {
		result := weightedOrderWithinPriority(channels)
		if len(result) != 2 {
			t.Fatalf("expected 2 channels, got %d", len(result))
		}
		if result[0].ID != "LO" {
			t.Errorf("run %d: expected LO (priority=1) first, got %s", i, result[0].ID)
		}
		if result[1].ID != "HI" {
			t.Errorf("run %d: expected HI (priority=2) second, got %s", i, result[1].ID)
		}
	}
}
