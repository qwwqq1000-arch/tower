package state

import "testing"

func TestSetLimitedReason_SetsReasonAndLimit(t *testing.T) {
	now := int64(1000)
	s := NewStore(fixedClock(now), minRand)
	s.Ensure("k", 2)

	untilMs := now + 5*3600*1000 // 5h from now
	s.SetLimitedReason("k", 2, untilMs, "5h")

	// Should be limited
	if !s.IsLimited("k", now) {
		t.Fatal("expected account to be limited")
	}

	// Snapshot should reflect reason
	snaps := s.Snapshot(now)
	found := false
	for _, sn := range snaps {
		if sn.Key == "k" {
			found = true
			if !sn.Limited {
				t.Error("snapshot: Limited should be true")
			}
			if sn.LimitReason != "5h" {
				t.Errorf("snapshot: LimitReason want '5h', got %q", sn.LimitReason)
			}
			if sn.LimitedUntil != untilMs {
				t.Errorf("snapshot: LimitedUntil want %d, got %d", untilMs, sn.LimitedUntil)
			}
		}
	}
	if !found {
		t.Fatal("account not in snapshot")
	}
}

func TestSetLimitedReason_ClearsWhenExpired(t *testing.T) {
	now := int64(1000)
	s := NewStore(fixedClock(now), minRand)
	s.Ensure("k", 2)

	untilMs := now + 1000 // expires 1s from now
	s.SetLimitedReason("k", 2, untilMs, "7d")

	// Before expiry
	if !s.IsLimited("k", now) {
		t.Fatal("should be limited before expiry")
	}

	// After expiry
	after := untilMs + 1
	if s.IsLimited("k", after) {
		t.Fatal("should not be limited after expiry")
	}

	// LimitReason in snapshot should be empty after expiry
	snaps := s.Snapshot(after)
	for _, sn := range snaps {
		if sn.Key == "k" {
			if sn.LimitReason != "" {
				t.Errorf("LimitReason should be empty after expiry, got %q", sn.LimitReason)
			}
			if sn.Limited {
				t.Error("Limited should be false after expiry")
			}
		}
	}
}

func TestSetLimited_KeepsReasonEmpty(t *testing.T) {
	now := int64(1000)
	s := NewStore(fixedClock(now), minRand)
	s.Ensure("k", 2)

	s.SetLimited("k", 2, map[string]int64{"all": now + 3600000})

	snaps := s.Snapshot(now)
	for _, sn := range snaps {
		if sn.Key == "k" {
			if sn.LimitReason != "" {
				t.Errorf("SetLimited should leave LimitReason empty, got %q", sn.LimitReason)
			}
			if !sn.Limited {
				t.Error("should be limited")
			}
		}
	}
}
