package state

import "testing"

func TestSlots_AcquireUpToCapacity(t *testing.T) {
	s := NewSlots(2)
	if s.Available(0) != 2 { t.Fatalf("avail=%d, want 2", s.Available(0)) }
	if !s.Acquire(0) || !s.Acquire(0) { t.Fatal("should acquire 2") }
	if s.Acquire(0) { t.Fatal("3rd acquire should fail at capacity") }
	if s.InUse() != 2 { t.Fatalf("inUse=%d, want 2", s.InUse()) }
}

func TestSlots_ReleaseCooldownBlocksReuse(t *testing.T) {
	s := NewSlots(1)
	s.Acquire(0)
	s.Release(1000, 500) // freed at 1000, cooling until 1500
	if s.Available(1000) != 0 { t.Fatal("slot should be cooling, 0 available") }
	if s.Acquire(1499) { t.Fatal("acquire during cooldown should fail") }
	if s.Available(1500) != 1 { t.Fatalf("avail at 1500=%d, want 1", s.Available(1500)) }
	if !s.Acquire(1500) { t.Fatal("acquire after cooldown should succeed") }
}

func TestSlots_ZeroCooldownImmediatelyReusable(t *testing.T) {
	s := NewSlots(1)
	s.Acquire(0)
	s.Release(100, 0)
	if s.Available(100) != 1 { t.Fatal("zero cooldown → immediately available") }
}

func TestSlots_ReleaseWhenNoneInUseNoop(t *testing.T) {
	s := NewSlots(1)
	s.Release(0, 500) // nothing in use
	if s.Available(0) != 1 { t.Fatalf("avail=%d, want 1 (release noop)", s.Available(0)) }
}
