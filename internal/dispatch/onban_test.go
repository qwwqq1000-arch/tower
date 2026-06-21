package dispatch

import (
	"context"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/state"
)

func TestOrchestrator_OnBanFires(t *testing.T) {
	st := state.NewStore(func() int64 { return 0 }, func(a, b int64) int64 { return a })
	st.Ensure("a", 1)
	var banned []string
	o := &Orchestrator{Store: st, Cfg: state.BreakerCfg{PersistStreak: 1, BaseMs: 1000, MaxMs: 9999, Mult: 2}, MaxAttempts: 5, OnBan: func(k string) { banned = append(banned, k) }}
	px := &stubProxy{results: map[string]ProxyResult{"a": {Status: 401, Banned: true}}}
	o.Dispatch(context.Background(), "opus", []string{"a"}, px)
	if len(banned) != 1 || banned[0] != "a" {
		t.Fatalf("OnBan fired=%v, want [a]", banned)
	}
}
