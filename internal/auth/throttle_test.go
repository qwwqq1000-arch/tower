package auth

import (
	"testing"
	"time"
)

func TestThrottleLocksAfterN(t *testing.T) {
	now := time.Unix(1000, 0)
	th := NewThrottle(5, time.Minute, 15*time.Minute)
	k := "alice|1.2.3.4"
	for i := 0; i < 5; i++ {
		if !th.Allowed(k, now) {
			t.Fatalf("attempt %d should be allowed", i)
		}
		th.RecordFailure(k, now)
	}
	if th.Allowed(k, now) {
		t.Fatal("should be locked after 5 fails")
	}
	if !th.Allowed(k, now.Add(16*time.Minute)) {
		t.Fatal("should unlock after lockout window")
	}
}

func TestThrottleResetOnSuccess(t *testing.T) {
	now := time.Unix(1000, 0)
	th := NewThrottle(5, time.Minute, 15*time.Minute)
	k := "bob|1.2.3.4"
	for i := 0; i < 4; i++ {
		th.RecordFailure(k, now)
	}
	th.Reset(k)
	if !th.Allowed(k, now) {
		t.Fatal("reset should clear failures")
	}
}
