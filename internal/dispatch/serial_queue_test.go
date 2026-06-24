package dispatch

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/state"
)

// TestSerialEffectiveCap verifies the effectiveCap helper (串行队列并发1).
func TestSerialEffectiveCap(t *testing.T) {
	if effectiveCap(true, 5) != 1 {
		t.Fatal("串行应=1")
	}
	if effectiveCap(false, 5) != 5 {
		t.Fatal("非串行=原值")
	}
}

// TestSerialWait_SlotFreedBeforeDispatch verifies that when SerialWaitKeys is wired
// and the slot is busy, the orchestrator waits for it to free up and then dispatches.
func TestSerialWait_SlotFreedBeforeDispatch(t *testing.T) {
	nowFn := func() int64 { return time.Now().UnixMilli() }
	st := state.NewStore(nowFn, func(min, max int64) int64 { return min })
	st.Ensure("k", 1)

	breaker := state.BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	// Acquire the sole slot so the orchestrator must wait.
	if !st.TryDispatch("k", "sonnet", breaker) {
		t.Fatal("initial dispatch should succeed")
	}

	var released atomic.Bool
	go func() {
		time.Sleep(40 * time.Millisecond)
		st.Complete("k", 0, 0) // release with zero cooldown
		released.Store(true)
	}()

	px := &stubProxy{results: map[string]ProxyResult{"k": {Status: 200, Body: "ok"}}}

	o := &Orchestrator{
		Store:          st,
		Cfg:            breaker,
		CooldownMin:    0,
		CooldownMax:    0,
		MaxAttempts:    3,
		NowMs:          nowFn,
		SerialWaitKeys: map[string]bool{"k": true},
		SerialWaitMs:   map[string]int64{"k": 500},
	}

	res, winKey, ok := o.Dispatch(context.Background(), "sonnet", []string{"k"}, px)
	if !ok {
		t.Fatalf("expected ok=true after slot freed, got res=%+v winKey=%q", res, winKey)
	}
	if winKey != "k" {
		t.Fatalf("winKey=%q, want %q", winKey, "k")
	}
	if !released.Load() {
		t.Fatal("goroutine should have released the slot before Dispatch returned")
	}
}

// TestSerialWait_Timeout verifies that when the slot never frees and wait times out,
// the orchestrator skips the account and returns false (no dispatch).
func TestSerialWait_Timeout(t *testing.T) {
	nowFn := func() int64 { return time.Now().UnixMilli() }
	st := state.NewStore(nowFn, func(min, max int64) int64 { return min })
	st.Ensure("k", 1)

	breaker := state.BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2}
	// Occupy the only slot and never release it.
	if !st.TryDispatch("k", "sonnet", breaker) {
		t.Fatal("initial dispatch should succeed")
	}

	px := &stubProxy{results: map[string]ProxyResult{"k": {Status: 200, Body: "ok"}}}

	o := &Orchestrator{
		Store:          st,
		Cfg:            breaker,
		CooldownMin:    0,
		CooldownMax:    0,
		MaxAttempts:    3,
		NowMs:          nowFn,
		SerialWaitKeys: map[string]bool{"k": true},
		SerialWaitMs:   map[string]int64{"k": 50}, // short deadline
	}

	_, _, ok := o.Dispatch(context.Background(), "sonnet", []string{"k"}, px)
	if ok {
		t.Fatal("expected ok=false: slot never freed, wait should time out")
	}
	if len(px.calls) > 0 {
		t.Fatalf("proxy should not have been called, but got calls=%v", px.calls)
	}
}
