package dispatch

import (
	"context"

	"github.com/qwwqq1000-arch/tower/internal/policy"
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
	// CooldownDist selects the inter-slot cooldown distribution: "uniform" (default)
	// uses CooldownMin/CooldownMax; "lognormal" uses CooldownP50/CooldownP95 (RangeI,
	// resolved per-key at Complete time).
	CooldownDist string
	CooldownP50  policy.RangeI // used when CooldownDist == "lognormal"
	CooldownP95  policy.RangeI // used when CooldownDist == "lognormal"
	MaxAttempts int
	OnBan       func(key string, status int)                 // optional: fired when an account is (re)banned
	OnRecover   func(key string)                             // optional: fired when a half-open trial succeeds (account recovers)
	OnAttempt   func(key string, res ProxyResult, ok bool) // optional: fired after each attempt (res carries Status/Body/Banned)
	IsCooldownSignal func(status int) bool                   // optional: status that cools (not bans) the account, e.g. 429

	// SerialWait: bounded slot-wait for accounts with SerialQueueEnabled.
	// When SerialWaitKeys[key] is true and SerialWaitMs[key] > 0, Dispatch waits
	// up to SerialWaitMs[key] ms for a free slot before skipping the account.
	// When nil/empty these maps add zero overhead (feature is off by default).
	NowMs          func() int64       // clock used for serial-wait deadline; nil disables feature
	SerialWaitKeys map[string]bool    // which keys have serial wait enabled
	SerialWaitMs   map[string]int64   // per-key wait deadline in ms
}

// attempt sends one request to key with guaranteed settlement. ok reports a clean
// 2xx; dispatched reports whether a slot was actually claimed and the request sent
// (false means the account was not contacted — breaker open/permanent/slot busy/
// rate-limited — so the caller must not log it as a failed attempt or count it).
func (o *Orchestrator) attempt(ctx context.Context, model, key string, px Proxy) (res ProxyResult, ok, dispatched bool) {
	d, trial := o.Store.TryDispatchTrial(key, model, o.Cfg)
	if !d {
		return ProxyResult{}, false, false
	}
	dispatched = true
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
		o.Store.CompleteDelay(key, o.CooldownDist,
			o.CooldownP50.Resolve(key, "p50"), o.CooldownP95.Resolve(key, "p95"),
			o.CooldownMin, o.CooldownMax)
		if !sendReturned {
			// proxy panicked — only release slot, no breaker penalty
			return
		}
		if trial {
			if !success && o.IsCooldownSignal != nil && o.IsCooldownSignal(res.Status) {
				// Transient cooldown signal (e.g. 429) during recovery: resolve the
				// trial without reopening/escalating; the error-cooldown owns backoff.
				o.Store.OnTrialCooldown(key)
			} else {
				o.Store.OnTrialResult(key, o.Cfg, success, res.Banned)
				if success {
					if o.OnRecover != nil {
						o.OnRecover(key)
					}
				} else if res.Banned && o.OnBan != nil {
					o.OnBan(key, res.Status)
				}
			}
		} else if success {
			o.Store.OnSuccess(key)
		} else if res.Banned {
			// Only a classified ban signal (per BanSignals/BanKeywords) advances the
			// breaker. Transient failures (502/429/network) fail over without banning.
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
		return ProxyResult{}, false, true
	}
	if r.Status >= 200 && r.Status < 300 && !r.Banned {
		return r, true, true
	}
	return r, false, true
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
		// Serial-wait: if this key has bounded wait enabled, block until a slot
		// frees or the deadline expires. On timeout we skip (same as slot-busy).
		// Zero overhead when feature is off (nil maps / NowMs not set).
		if o.NowMs != nil && o.SerialWaitKeys[key] {
			if waitMs := o.SerialWaitMs[key]; waitMs > 0 {
				if !o.Store.WaitForSlot(ctx, key, o.NowMs()+waitMs, o.NowMs) {
					continue
				}
			}
		}
		res, ok, dispatched := o.attempt(ctx, model, key, px)
		if !dispatched {
			// account not contacted (breaker open/permanent/slot busy/limited) —
			// not a real attempt: don't count it, log it, or emit a retry event.
			continue
		}
		attempts++
		if o.OnAttempt != nil {
			o.OnAttempt(key, res, ok)
		}
		if ok {
			return res, key, true
		}
		last = res
	}
	return last, "", false
}
