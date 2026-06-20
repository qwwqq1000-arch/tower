package state

import (
	"sync"
	"testing"
)

func fixedClock(v int64) func() int64 { return func() int64 { return v } }
func minRand(min, max int64) int64     { return min }

func TestStore_TryDispatchRespectsCapacity(t *testing.T) {
	s := NewStore(fixedClock(0), minRand)
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	s.Ensure("k", 2)
	if !s.TryDispatch("k", "opus", c) { t.Fatal("1st dispatch should succeed") }
	if !s.TryDispatch("k", "opus", c) { t.Fatal("2nd dispatch should succeed") }
	if s.TryDispatch("k", "opus", c) { t.Fatal("3rd should fail at capacity 2") }
	s.Complete("k", 0, 0) // release one, zero cooldown
	if !s.TryDispatch("k", "opus", c) { t.Fatal("after complete should dispatch again") }
}

func TestStore_BanThenRecover(t *testing.T) {
	s := NewStore(fixedClock(100), minRand)
	c := BreakerCfg{PersistStreak: 2, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	s.Ensure("k", 1)
	s.OnBanSignal("k", c)
	if opened := s.OnBanSignal("k", c); !opened { t.Fatal("2nd signal should open") }
	if s.TryDispatch("k", "opus", c) { t.Fatal("banned account should not dispatch") }
	s.OnSuccess("k")
	if !s.TryDispatch("k", "opus", c) { t.Fatal("after success should dispatch") }
}

func TestStore_ConcurrentDispatchComplete(t *testing.T) {
	s := NewStore(fixedClock(0), minRand)
	c := BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	s.Ensure("k", 4)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.TryDispatch("k", "opus", c) {
				s.Complete("k", 0, 0)
			}
		}()
	}
	wg.Wait()
	// after all complete, capacity should be fully free again
	if got := s.Ensure("k", 4).Slots.Available(0); got != 4 {
		t.Fatalf("available=%d, want 4 after all complete", got)
	}
}
