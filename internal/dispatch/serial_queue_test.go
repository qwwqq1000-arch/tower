package dispatch

import "testing"

// TestSerialEffectiveCap verifies the effectiveCap helper (串行队列并发1).
func TestSerialEffectiveCap(t *testing.T) {
	if effectiveCap(true, 5) != 1 {
		t.Fatal("串行应=1")
	}
	if effectiveCap(false, 5) != 5 {
		t.Fatal("非串行=原值")
	}
}
