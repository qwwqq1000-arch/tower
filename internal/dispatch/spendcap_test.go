package dispatch

import (
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/state"
)

// TestSpendCapOverCap tests the overCap pure helper function.
func TestSpendCapOverCap(t *testing.T) {
	if !overCap(150, 140) {
		t.Fatal("150>=140 应到顶")
	}
	if overCap(100, 140) {
		t.Fatal("100<140 不应到顶")
	}
	// capUsd=0 means disabled — never over cap
	if overCap(9999, 0) {
		t.Fatal("cap=0 (disabled) 不应到顶")
	}
	// exactly at cap is over
	if !overCap(140, 140) {
		t.Fatal("140>=140 应到顶")
	}
}

// TestSpendCapStoreRoundTrip tests Store.AddSpend + Store.SpendInWindow.
func TestSpendCapStoreRoundTrip(t *testing.T) {
	now := int64(1_000_000_000)
	clock := func() int64 { return now }
	rnd := func(min, max int64) int64 { return min }
	store := state.NewStore(clock, rnd)

	key := "node1:profile1"
	window5h := int64(18_000_000) // 5h in ms
	window7d := int64(604_800_000)

	// No entries yet — should return 0
	if got := store.SpendInWindow(key, now, window5h); got != 0 {
		t.Fatalf("expected 0 before any spend, got %f", got)
	}

	// Add some spend
	store.AddSpend(key, 10.0, now)
	store.AddSpend(key, 5.0, now)

	// Both entries are within the 5h window
	got := store.SpendInWindow(key, now, window5h)
	if got != 15.0 {
		t.Fatalf("expected 15.0 within 5h window, got %f", got)
	}

	// Both entries are within the 7d window too
	got7d := store.SpendInWindow(key, now, window7d)
	if got7d != 15.0 {
		t.Fatalf("expected 15.0 within 7d window, got %f", got7d)
	}

	// Add an old entry (outside 5h window but inside 7d window)
	oldTs := now - window5h - 1000 // just outside 5h
	store.AddSpend(key, 20.0, oldTs)

	// 5h window should still be 15 (old entry excluded)
	got5h := store.SpendInWindow(key, now, window5h)
	if got5h != 15.0 {
		t.Fatalf("expected 15.0 within 5h window after old entry, got %f", got5h)
	}

	// 7d window should include old entry: 35
	got7dAll := store.SpendInWindow(key, now, window7d)
	if got7dAll != 35.0 {
		t.Fatalf("expected 35.0 within 7d window, got %f", got7dAll)
	}
}
