package billing

import "testing"

func TestOutstandingToSettle(t *testing.T) {
	if got := OutstandingToSettle(10, 4); got != 6 {
		t.Fatalf("first settle: got %v want 6", got)
	}
	// re-settle after fully settled → 0 (no double charge)
	if got := OutstandingToSettle(10, 10); got != 0 {
		t.Fatalf("re-settle: got %v want 0", got)
	}
	// over-settled clamps to 0
	if got := OutstandingToSettle(10, 12); got != 0 {
		t.Fatalf("clamp: got %v want 0", got)
	}
}

func TestRoundUSD(t *testing.T) {
	if got := RoundUSD(63.34780276499996); got != 63.35 {
		t.Fatalf("got %v want 63.35", got)
	}
}
