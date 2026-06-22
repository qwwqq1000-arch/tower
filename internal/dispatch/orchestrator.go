package dispatch

import (
	"context"

	"github.com/qwwqq1000-arch/tower/internal/state"
)

// ProxyResult is the outcome of forwarding a request to one account.
type ProxyResult struct {
	Status int
	Body   string
	Banned bool // ban signal (401/403/keyword) classified by the proxy
}

// Proxy forwards a request to the account identified by key.
type Proxy interface {
	Send(ctx context.Context, key string) (ProxyResult, error)
}

// Orchestrator drives selection → proxy → ban-detection → failover, guaranteeing
// every dispatched attempt is settled on the state store (slot released + breaker
// resolved) via defer — so a half-open trial can never wedge an account.
type Orchestrator struct {
	Store       *state.Store
	Cfg         state.BreakerCfg
	CooldownMin int64
	CooldownMax int64
	MaxAttempts int
	OnBan       func(key string, status int)                 // optional: fired when an account is (re)banned
	OnAttempt   func(key string, status int, ok bool, banned bool) // optional: fired after each attempt
}

// attempt sends one request to key with guaranteed settlement. ok reports a clean 2xx.
func (o *Orchestrator) attempt(ctx context.Context, model, key string, px Proxy) (res ProxyResult, ok bool) {
	dispatched, trial := o.Store.TryDispatchTrial(key, model, o.Cfg)
	if !dispatched {
		return ProxyResult{}, false
	}
	// sendReturned tracks whether Send returned normally (vs panicked).
	// On panic we release the slot but skip the ban signal — a proxy crash is
	// not a ban signal and must not open the breaker.
	sendReturned := false
	settled := false
	settle := func(success bool) {
		if settled {
			return
		}
		settled = true
		o.Store.Complete(key, o.CooldownMin, o.CooldownMax)
		if !sendReturned {
			// proxy panicked — only release slot, no breaker penalty
			return
		}
		if trial {
			o.Store.OnTrialResult(key, o.Cfg, success)
			if !success && o.OnBan != nil {
				o.OnBan(key, res.Status)
			}
		} else if success {
			o.Store.OnSuccess(key)
		} else {
			if o.Store.OnBanSignal(key, o.Cfg) && o.OnBan != nil {
				o.OnBan(key, res.Status)
			}
		}
	}
	// defer guarantees settlement even on panic; success path overrides before return.
	defer func() { settle(ok) }()

	r, err := px.Send(ctx, key)
	sendReturned = true
	if err != nil {
		return ProxyResult{}, false
	}
	if r.Status >= 200 && r.Status < 300 && !r.Banned {
		return r, true
	}
	return r, false
}

// Dispatch tries accounts in order until one returns a clean 2xx or attempts run out.
// It returns the ProxyResult, the key that succeeded ("" on failure), and a bool ok.
func (o *Orchestrator) Dispatch(ctx context.Context, model string, order []string, px Proxy) (ProxyResult, string, bool) {
	var last ProxyResult
	attempts := 0
	for _, key := range order {
		if attempts >= o.MaxAttempts {
			break
		}
		attempts++
		res, ok := o.attempt(ctx, model, key, px)
		if o.OnAttempt != nil {
			o.OnAttempt(key, res.Status, ok, res.Banned)
		}
		if ok {
			return res, key, true
		}
		last = res
	}
	return last, "", false
}
