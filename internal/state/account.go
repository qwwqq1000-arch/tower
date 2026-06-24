package state

import "strings"

type spendEntry struct {
	ts  int64
	usd float64
}

// classOf maps a model identifier to its rate-limit class ("opus", "sonnet",
// "haiku", or "all" for unknown/unrecognized models). The comparison is
// case-insensitive substring matching so both raw class names ("opus") and
// full model IDs ("claude-opus-4-8") resolve to the same class.
func classOf(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "opus"):
		return "opus"
	case strings.Contains(m, "sonnet"):
		return "sonnet"
	case strings.Contains(m, "haiku"):
		return "haiku"
	default:
		return "all"
	}
}

// Account aggregates the operational state of one (node, profile) account.
type Account struct {
	Breaker      Breaker
	Slots        *Slots
	Disabled     bool
	Offline      bool
	LimitedUntil map[string]int64 // model class ("opus"/"sonnet"/"all") -> reset time ms
	WarmupCap    int              // 0 = no warmup limit; >0 = max in-flight during warmup
	CoolUntil    int64            // ms; temporary error-cooldown (e.g. 429); 0 = none. Ephemeral.
	spendLog     []spendEntry     // 升序时间
	reqLog       []int64          // timestamps (ms, ascending) of dispatched requests; for rate governor
}

// RecordReq appends a request timestamp and prunes entries older than 1 day (86400000 ms).
// The 1-day pruning window is the longest rate window, ensuring shorter windows (1min, 1hr)
// still have complete data.
func (a *Account) RecordReq(now int64) {
	a.reqLog = append(a.reqLog, now)
	cut := now - 86400000
	i := 0
	for i < len(a.reqLog) && a.reqLog[i] < cut {
		i++
	}
	if i > 0 {
		a.reqLog = a.reqLog[i:]
	}
}

// ReqsInWindow counts request entries in the closed window [now-windowMs, now].
func (a *Account) ReqsInWindow(now, windowMs int64) int {
	cut := now - windowMs
	count := 0
	for _, ts := range a.reqLog {
		if ts >= cut && ts <= now {
			count++
		}
	}
	return count
}

// NewAccount builds an account with a slot set of the given capacity.
func NewAccount(capacity int) *Account {
	return &Account{Slots: NewSlots(capacity), LimitedUntil: map[string]int64{}}
}

// Status returns the account's headline state at now
// (priority: disabled > offline > permanent > banned > half_open > active).
func (a *Account) Status(now int64) string {
	if a.Disabled {
		return "disabled"
	}
	if a.Offline {
		return "offline"
	}
	bs := a.Breaker.State(now)
	if bs == "permanent" {
		return "permanent"
	}
	// An active error-cooldown (e.g. 429) takes precedence over a recoverable
	// breaker state when it outlasts the breaker's recovery time, so a rate-limit
	// shows as 限流·冷却 for its full duration instead of a shorter 封控·冷却.
	if now < a.CoolUntil && a.CoolUntil >= a.Breaker.RecoverAt() {
		return "cooldown"
	}
	switch bs {
	case "open":
		return "banned"
	case "half_open":
		return "half_open"
	}
	return "active"
}

// limitedFor reports whether the model class is rate-limited at now.
// It checks the "all" key (applies to every model), the exact model string
// (for backward compatibility), and the normalized class returned by classOf
// so that a limit keyed by "opus" matches full model IDs like "claude-opus-4-8".
func (a *Account) limitedFor(now int64, model string) bool {
	if a.LimitedUntil == nil {
		return false
	}
	if until, ok := a.LimitedUntil["all"]; ok && now < until {
		return true
	}
	if until, ok := a.LimitedUntil[model]; ok && now < until {
		return true
	}
	if cls := classOf(model); cls != model {
		if until, ok := a.LimitedUntil[cls]; ok && now < until {
			return true
		}
	}
	return false
}

// LimitState reports whether the account is currently quota-limited (rotated out
// of dispatch for any model class) and the latest active reset deadline. Used by
// Snapshot so the UI can surface a quota-rotated account as "限额" — the breaker
// Status() is independent and stays "active" while the account is rate-limited.
func (a *Account) LimitState(now int64) (bool, int64) {
	var until int64
	for _, t := range a.LimitedUntil {
		if t > now && t > until {
			until = t
		}
	}
	return until > now, until
}

// CanDispatch reports whether the account may take a request for model at now,
// and whether this dispatch would be a half-open recovery trial. It has no side
// effects; the caller claims the trial via Breaker.TakeTrial when trial is true.
func (a *Account) CanDispatch(now int64, model string, cfg BreakerCfg) (ok bool, trial bool) {
	if a.Disabled {
		return false, false
	}
	if a.Offline {
		return false, false
	}
	if now < a.CoolUntil { // temporary error-cooldown (e.g. 429)
		return false, false
	}
	if a.limitedFor(now, model) {
		return false, false
	}
	if a.Slots.Available(now) <= 0 {
		return false, false
	}
	// Warmup cap: effective concurrency = min(slot capacity, WarmupCap).
	if a.WarmupCap > 0 && a.Slots.InUse() >= a.WarmupCap {
		return false, false
	}
	switch a.Breaker.State(now) {
	case "closed":
		return true, false
	case "half_open":
		// Only eligible if no trial is already in flight.
		if a.Breaker.trial {
			return false, false
		}
		return true, true
	default: // open
		return false, false
	}
}

// AddSpend records a spend entry and prunes entries older than (now-pruneWindowMs).
// The caller should pass the longest applicable window (e.g., 7d) to avoid pruning
// data that is still needed for shorter windows.
func (a *Account) AddSpend(now int64, usd float64, pruneWindowMs int64) {
	a.spendLog = append(a.spendLog, spendEntry{ts: now, usd: usd})
	cut := now - pruneWindowMs
	i := 0
	for i < len(a.spendLog) && a.spendLog[i].ts < cut {
		i++
	}
	if i > 0 {
		a.spendLog = a.spendLog[i:]
	}
}

// SpendInWindow returns the sum of spend entries in [now-windowMs, now].
func (a *Account) SpendInWindow(now, windowMs int64) float64 {
	cut := now - windowMs
	var sum float64
	for _, e := range a.spendLog {
		if e.ts > cut {
			sum += e.usd
		}
	}
	return sum
}
