package state

import (
	"sync/atomic"
	"testing"
	"time"
)

// clockMs returns a real-time clock in milliseconds (for wait tests).
func realClock() func() int64 {
	return func() int64 { return time.Now().UnixMilli() }
}

func TestWaitForSlot_AlreadyFree(t *testing.T) {
	s := NewStore(realClock(), minRand)
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	s.Ensure("k", 1)
	// Slot is free — WaitForSlot should return true immediately.
	now := time.Now().UnixMilli()
	got := s.WaitForSlot("k", now+200, func() int64 { return time.Now().UnixMilli() })
	if !got {
		t.Fatal("expected true: slot is free")
	}
	_ = c
}

func TestWaitForSlot_FreesInTime(t *testing.T) {
	s := NewStore(realClock(), minRand)
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	s.Ensure("k", 1)
	// Acquire the only slot.
	if !s.TryDispatch("k", "sonnet", c) {
		t.Fatal("initial dispatch should succeed")
	}
	// Slot is now full (cap=1, inUse=1).
	// Release it after 30ms in a background goroutine.
	var released atomic.Bool
	go func() {
		time.Sleep(30 * time.Millisecond)
		s.Complete("k", 0, 0) // release with zero cooldown
		released.Store(true)
	}()
	now := time.Now().UnixMilli()
	got := s.WaitForSlot("k", now+200, func() int64 { return time.Now().UnixMilli() })
	if !got {
		t.Fatal("expected true: slot should have been released before deadline")
	}
	if !released.Load() {
		t.Fatal("slot should have been released by goroutine before WaitForSlot returned")
	}
}

func TestWaitForSlot_Timeout(t *testing.T) {
	s := NewStore(realClock(), minRand)
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	s.Ensure("k", 1)
	// Acquire the only slot and never release it.
	if !s.TryDispatch("k", "sonnet", c) {
		t.Fatal("initial dispatch should succeed")
	}
	now := time.Now().UnixMilli()
	got := s.WaitForSlot("k", now+50, func() int64 { return time.Now().UnixMilli() })
	if got {
		t.Fatal("expected false: slot stays busy and deadline should expire")
	}
}
