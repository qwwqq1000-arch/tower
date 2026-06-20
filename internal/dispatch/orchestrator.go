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
}

// attempt sends one request to key with guaranteed settlement. ok reports a clean 2xx.
func (o *Orchestrator) attempt(ctx context.Context, model, key string, px Proxy) (res ProxyResult, ok bool) {
	if !o.Store.TryDispatch(key, model, o.Cfg) {
		return ProxyResult{}, false
	}
	settled := false
	settle := func(success bool) {
		if settled {
			return
		}
		settled = true
		o.Store.Complete(key, o.CooldownMin, o.CooldownMax)
		if success {
			o.Store.OnSuccess(key)
		} else {
			o.Store.OnBanSignal(key, o.Cfg)
		}
	}
	// defer guarantees settlement even on panic; success path overrides before return.
	defer func() { settle(ok) }()

	r, err := px.Send(ctx, key)
	if err != nil {
		return ProxyResult{}, false
	}
	if r.Status >= 200 && r.Status < 300 && !r.Banned {
		return r, true
	}
	return r, false
}

// Dispatch tries accounts in order until one returns a clean 2xx or attempts run out.
func (o *Orchestrator) Dispatch(ctx context.Context, model string, order []string, px Proxy) (ProxyResult, bool) {
	var last ProxyResult
	attempts := 0
	for _, key := range order {
		if attempts >= o.MaxAttempts {
			break
		}
		attempts++
		res, ok := o.attempt(ctx, model, key, px)
		if ok {
			return res, true
		}
		last = res
	}
	return last, false
}
