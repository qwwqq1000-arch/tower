package dispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/state"
)

type stubProxy struct {
	results map[string]ProxyResult
	errs    map[string]error
	calls   []string
}

func (s *stubProxy) Send(_ context.Context, key string) (ProxyResult, error) {
	s.calls = append(s.calls, key)
	if e := s.errs[key]; e != nil {
		return ProxyResult{}, e
	}
	return s.results[key], nil
}

func newOrch() *Orchestrator {
	st := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	return &Orchestrator{
		Store:       st,
		Cfg:         state.BreakerCfg{PersistStreak: 1, BaseMs: 1000, MaxMs: 9999, Mult: 2},
		CooldownMin: 0, CooldownMax: 0, MaxAttempts: 5,
	}
}

func swapClock(st *state.Store, t int64) *state.Store {
	st.SetClock(func() int64 { return t })
	return st
}

func TestDispatch_SuccessFirst(t *testing.T) {
	o := newOrch()
	o.Store.Ensure("a", 1)
	px := &stubProxy{results: map[string]ProxyResult{"a": {Status: 200, Body: "ok"}}}
	res, ok := o.Dispatch(context.Background(), "opus", []string{"a"}, px)
	if !ok || res.Status != 200 {
		t.Fatalf("res=%+v ok=%v", res, ok)
	}
	// slot released after complete → can dispatch again
	if !o.Store.TryDispatch("a", "opus", o.Cfg) {
		t.Fatal("slot should be released after success")
	}
}

func TestDispatch_FailoverOnBan(t *testing.T) {
	o := newOrch()
	o.Store.Ensure("a", 1)
	o.Store.Ensure("b", 1)
	px := &stubProxy{results: map[string]ProxyResult{
		"a": {Status: 401, Banned: true},
		"b": {Status: 200, Body: "ok"},
	}}
	res, ok := o.Dispatch(context.Background(), "opus", []string{"a", "b"}, px)
	if !ok || res.Status != 200 {
		t.Fatalf("should failover to b: res=%+v ok=%v", res, ok)
	}
	if len(px.calls) != 2 || px.calls[0] != "a" || px.calls[1] != "b" {
		t.Fatalf("calls=%v, want [a b]", px.calls)
	}
	// a got a ban signal (PersistStreak=1 → opened) → not dispatchable
	if o.Store.TryDispatch("a", "opus", o.Cfg) {
		t.Fatal("a should be banned after ban signal")
	}
}

func TestDispatch_AllFail(t *testing.T) {
	o := newOrch()
	o.Store.Ensure("a", 1)
	px := &stubProxy{errs: map[string]error{"a": errors.New("boom")}}
	_, ok := o.Dispatch(context.Background(), "opus", []string{"a"}, px)
	if ok {
		t.Fatal("should return ok=false when all fail")
	}
}

func TestDispatch_TrialAlwaysResolved_NoWedge(t *testing.T) {
	o := newOrch()
	o.Store.Ensure("a", 1)
	// open the breaker, then advance time so it's half_open
	o.Store.OnBanSignal("a", o.Cfg) // opens until 1000 (now=0)
	o.Store = swapClock(o.Store, 2000) // now half_open at t=2000
	// a trial that fails must reopen — and crucially must not leave trial stuck
	px := &stubProxy{results: map[string]ProxyResult{"a": {Status: 403, Banned: true}}}
	o.Dispatch(context.Background(), "opus", []string{"a"}, px)
	// advance again to half_open and confirm a NEW trial can be taken (not wedged)
	o.Store = swapClock(o.Store, 9_999_999)
	if !o.Store.TryDispatch("a", "opus", o.Cfg) {
		t.Fatal("after failed trial + cooldown, a new trial must be possible (no wedge)")
	}
}
