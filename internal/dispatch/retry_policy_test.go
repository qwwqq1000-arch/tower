package dispatch

import (
	"context"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

// ─── directFallbackMatch tests ────────────────────────────────────────────────

func TestDirectFallbackMatch_MatchesStatusAndKeyword(t *testing.T) {
	cfg := policy.Config{
		DirectFallbackStatusCodes: []int{400},
		DirectFallbackKeywords:    []string{"rate_limit_error"},
	}
	body := `{"error":{"type":"rate_limit_error","message":"concurrency limit reached"}}`
	if !directFallbackMatch(400, body, cfg) {
		t.Fatal("expected match on 400 + rate_limit_error")
	}
}

func TestDirectFallbackMatch_NoMatchWhenStatusNotInCodes(t *testing.T) {
	cfg := policy.Config{
		DirectFallbackStatusCodes: []int{400},
		DirectFallbackKeywords:    []string{"rate_limit_error"},
	}
	body := `{"error":{"type":"rate_limit_error"}}`
	if directFallbackMatch(429, body, cfg) {
		t.Fatal("should NOT match: status 429 not in codes [400]")
	}
}

func TestDirectFallbackMatch_NoMatchWhenKeywordAbsent(t *testing.T) {
	cfg := policy.Config{
		DirectFallbackStatusCodes: []int{400},
		DirectFallbackKeywords:    []string{"rate_limit_error"},
	}
	body := `{"error":{"type":"invalid_request_error","message":"bad param"}}`
	if directFallbackMatch(400, body, cfg) {
		t.Fatal("should NOT match: keyword absent from body")
	}
}

func TestDirectFallbackMatch_NoMatchWhenCodesEmpty(t *testing.T) {
	cfg := policy.Config{
		DirectFallbackStatusCodes: nil,
		DirectFallbackKeywords:    []string{"rate_limit_error"},
	}
	if directFallbackMatch(400, `{"error":{"type":"rate_limit_error"}}`, cfg) {
		t.Fatal("should NOT match: codes empty → feature off")
	}
}

func TestDirectFallbackMatch_NoMatchWhenKeywordsEmpty(t *testing.T) {
	cfg := policy.Config{
		DirectFallbackStatusCodes: []int{400},
		DirectFallbackKeywords:    nil,
	}
	if directFallbackMatch(400, `{"error":{"type":"rate_limit_error"}}`, cfg) {
		t.Fatal("should NOT match: keywords empty → feature off")
	}
}

func TestDirectFallbackMatch_CaseInsensitive(t *testing.T) {
	cfg := policy.Config{
		DirectFallbackStatusCodes: []int{400},
		DirectFallbackKeywords:    []string{"Rate_Limit_Error"},
	}
	if !directFallbackMatch(400, `{"error":{"type":"rate_limit_error"}}`, cfg) {
		t.Fatal("keyword match must be case-insensitive")
	}
}

// ─── Orchestrator: DirectFallback stops at first matching account ──────────────

// countingProxy records calls and returns a configurable result per key.
type countingProxy struct {
	results map[string]ProxyResult
	calls   []string
}

func (p *countingProxy) Send(_ context.Context, key string) (ProxyResult, error) {
	p.calls = append(p.calls, key)
	return p.results[key], nil
}

func TestOrchestrator_DirectFallback_StopsOnMatch(t *testing.T) {
	st := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	st.Ensure("a", 1)
	st.Ensure("b", 1)

	px := &countingProxy{results: map[string]ProxyResult{
		"a": {Status: 400, Body: `{"error":{"type":"rate_limit_error"}}`},
		"b": {Status: 200, Body: "ok"},
	}}

	triggered := false
	o := &Orchestrator{
		Store:       st,
		Cfg:         state.BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2},
		CooldownMin: 0, CooldownMax: 0, MaxAttempts: 5,
		DirectFallback: func(res ProxyResult) bool {
			return directFallbackMatch(res.Status, res.Body, policy.Config{
				DirectFallbackStatusCodes: []int{400},
				DirectFallbackKeywords:    []string{"rate_limit_error"},
			})
		},
		OnAttempt: func(key string, res ProxyResult, ok bool) {
			if !ok && key == "a" {
				triggered = true
			}
		},
	}

	_, _, ok := o.Dispatch(context.Background(), "opus", []string{"a", "b"}, px)
	if ok {
		t.Fatal("DirectFallback should return ok=false without trying next account")
	}
	if len(px.calls) != 1 || px.calls[0] != "a" {
		t.Fatalf("expected only account 'a' to be called, got: %v", px.calls)
	}
	if !triggered {
		t.Fatal("OnAttempt must fire for the failed account before DirectFallback exits")
	}
}

func TestOrchestrator_DirectFallback_NormalFallover_WhenNoMatch(t *testing.T) {
	// A plain 400 WITHOUT the keyword still fails over normally (not direct-fallback).
	st := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	st.Ensure("a", 1)
	st.Ensure("b", 1)

	px := &countingProxy{results: map[string]ProxyResult{
		"a": {Status: 400, Body: `{"error":{"type":"invalid_request_error"}}`},
		"b": {Status: 200, Body: "ok"},
	}}

	o := &Orchestrator{
		Store:       st,
		Cfg:         state.BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2},
		CooldownMin: 0, CooldownMax: 0, MaxAttempts: 5,
		DirectFallback: func(res ProxyResult) bool {
			return directFallbackMatch(res.Status, res.Body, policy.Config{
				DirectFallbackStatusCodes: []int{400},
				DirectFallbackKeywords:    []string{"rate_limit_error"},
			})
		},
	}

	_, winKey, ok := o.Dispatch(context.Background(), "opus", []string{"a", "b"}, px)
	if !ok {
		t.Fatal("plain 400 without keyword must fail over normally and succeed on b")
	}
	if winKey != "b" {
		t.Fatalf("expected winKey=b, got %q", winKey)
	}
	if len(px.calls) != 2 {
		t.Fatalf("expected 2 calls (a then b), got: %v", px.calls)
	}
}

// ─── Orchestrator: RetrySameAccountMax ────────────────────────────────────────

func TestOrchestrator_RetrySameAccountMax_RetriesTwice(t *testing.T) {
	st := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	st.Ensure("a", 5) // high cap so retries don't hit slot limits

	callCount := 0
	px := &callCountProxy{
		onCall: func(key string) ProxyResult {
			callCount++
			if callCount < 2 {
				return ProxyResult{Status: 500, Body: "server error"}
			}
			return ProxyResult{Status: 200, Body: "ok"}
		},
	}

	o := &Orchestrator{
		Store:             st,
		Cfg:               state.BreakerCfg{PersistStreak: 3, BaseMs: 1000, MaxMs: 9999, Mult: 2},
		CooldownMin:       0, CooldownMax: 0, MaxAttempts: 5,
		RetrySameAccountMax: 2,
	}

	_, winKey, ok := o.Dispatch(context.Background(), "opus", []string{"a"}, px)
	if !ok {
		t.Fatal("expected success after same-account retry")
	}
	if winKey != "a" {
		t.Fatalf("expected winKey=a, got %q", winKey)
	}
	if callCount < 2 {
		t.Fatalf("expected at least 2 calls to same account, got %d", callCount)
	}
}

// callCountProxy calls onCall for every Send.
type callCountProxy struct {
	onCall func(key string) ProxyResult
}

func (p *callCountProxy) Send(_ context.Context, key string) (ProxyResult, error) {
	return p.onCall(key), nil
}
