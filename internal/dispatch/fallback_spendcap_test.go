package dispatch

import "testing"

// TestFallbackOverDailyCap verifies that overCap correctly detects over-daily-cap.
func TestFallbackOverDailyCap(t *testing.T) {
	if !overCap(120, 100) {
		t.Fatal("120>=100 应触发日上限")
	}
}

// TestFallbackOverTotalCap verifies that overCap correctly detects over-total-cap.
func TestFallbackOverTotalCap(t *testing.T) {
	if !overCap(500, 200) {
		t.Fatal("500>=200 应触发总上限")
	}
}

// TestFallbackCapZeroNeverSkips verifies that cap=0 (disabled) never triggers skip.
func TestFallbackCapZeroNeverSkips(t *testing.T) {
	if overCap(9999, 0) {
		t.Fatal("cap=0 (disabled) 不应触发跳过")
	}
}

// TestFallbackBelowCapNotSkipped verifies that spend below cap does not trigger skip.
func TestFallbackBelowCapNotSkipped(t *testing.T) {
	if overCap(50, 100) {
		t.Fatal("50<100 未到上限不应跳过")
	}
}

// TestFallbackAtCapSkipped verifies that spend exactly at cap triggers skip.
func TestFallbackAtCapSkipped(t *testing.T) {
	if !overCap(100, 100) {
		t.Fatal("100>=100 应触发上限")
	}
}
