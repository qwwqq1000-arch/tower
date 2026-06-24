// Package policy resolves the three-layer 封控 configuration (global → group/tenant
// → node/account) and produces dry-run diffs for the UI.
package policy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PickMaxConcurrent extracts MaxConcurrent from a JSON policy patch (the global
// policy row's params). Returns def when the patch is absent, unparseable, or does
// not override MaxConcurrent with a positive value. Shared by the meridian poller
// and the CPA discovery loop so per-account slot capacity matches across kinds.
func PickMaxConcurrent(patchJSON []byte, def int) int {
	var p Patch
	if err := json.Unmarshal(patchJSON, &p); err != nil {
		return def
	}
	if p.MaxConcurrent != nil && *p.MaxConcurrent > 0 {
		return *p.MaxConcurrent
	}
	return def
}

// Config is the resolved 封控 configuration (representative core fields).
type Config struct {
	MaxConcurrent             int
	SlotCooldownMinMs         int64
	SlotCooldownMaxMs         int64
	BanPersistStreak          int
	PermanentBanStreak        int // consecutive ban signals → permanent ban (0 = never); takes precedence
	CooldownBaseMs            int64
	CooldownMaxMs             int64
	CooldownMult              float64
	AffinityTTLSec            int
	FallbackEnabled           bool
	FallbackPriceThresholdUsd float64
	FallbackKeywords          []string
	FallbackModels            []string
	FallbackProbeEnabled      bool
	BanSignals                []int
	BanKeywords               []string
	// CooldownSignals are HTTP status codes (e.g. 429) that temporarily cool the
	// account for CooldownSignalSec seconds (NOT a ban — it auto-recovers and never
	// escalates to permanent). Empty = off.
	CooldownSignals   []int
	CooldownSignalSec int
	MaxFailover       int

	// Warmup: new accounts (onboarded within WarmupHours) serve at reduced concurrency.
	// 0 = off.
	WarmupHours         int
	WarmupMaxConcurrent int
	WarmupBlockOpus     bool

	// Elastic scaling: activate reserve accounts when baseline is saturated.
	// Reserves (accounts with non-baseline roles) are only activated if ElasticEnabled is true
	// and the baseline accounts exceed ElasticScaleUpUtil. Accounts with baseline roles always
	// remain active; only non-baseline accounts are dynamically scaled.
	ElasticEnabled       bool
	ElasticBaselineCount int     // number of accounts that form the active baseline; default 1
	ElasticScaleUpUtil   float64 // utilisation threshold to activate reserves (0.0–1.0); default 0.8
	ElasticScaleDownUtil float64 // utilisation threshold to release reserves (hysteresis); default 0.3
	ElasticMaxReserve    int     // cap on reserve accounts added per evaluation; default 1000

	// Session exile: route a conversation to fallback after this many consecutive
	// errors from our nodes. 0 = disabled.
	SessionErrorThreshold int
	// SessionCooldownSec is the duration (in seconds) a conversation stays exiled.
	// Applies to both session-error exile and response-refusal exile. Default 300.
	SessionCooldownSec int
	// ResponseExileEnabled enables detection of Claude safety-refusal responses.
	ResponseExileEnabled bool
	// ResponseExileKeywords are substrings (case-insensitive) that identify a
	// safety-refusal body. Matched conversation is force-exiled to fallback.
	ResponseExileKeywords []string

	// StreamIdleTimeoutSec is the maximum number of seconds allowed between
	// successive bytes on a streaming response. When the upstream stalls longer
	// than this, the idle-timeout reader fires, the read returns an error, and
	// the dispatch slot is released (dispatch-core-6). 0 means no idle timeout.
	// Default 120.
	StreamIdleTimeoutSec int

	// QuotaLimitKeywords are case-insensitive substrings that, when found in an ERROR
	// response from an account, mark it quota-limited (rotated out until the parsed
	// reset, or a 1h default). This is error + keyword — a bare error or a transient
	// rate_limit_error must NOT limit the account; only a real usage-exhaustion message
	// (e.g. "hit your limit · resets …") does (account-limit-reactive). Empty = off.
	QuotaLimitKeywords []string

	// QuotaLimitStatusCodes gates the keyword scan to specific HTTP status codes so it
	// does not run the body scan on every response (perf). Only responses whose status
	// is in this set are scanned for QuotaLimitKeywords. Empty = scan all error
	// responses. Defaults to the codes a quota limit actually arrives on (429 + 500).
	QuotaLimitStatusCodes []int

	// ModelMaxTokens caps the requested output tokens (the body's max_tokens) per
	// model. A request whose max_tokens exceeds the matched ceiling is rejected with
	// a 400 BEFORE any node/fallback attempt (no retry) — an over-limit request fails
	// on every upstream, so retrying wastes attempts (limits-1). Keys match a model
	// by longest substring (e.g. "claude-opus-4-8" matches "claude-opus-4-8" and any
	// dated suffix). Empty/zero ceiling = unlimited for that model.
	ModelMaxTokens map[string]int

	// QuietHoursTZ is the IANA timezone used when evaluating slot-window
	// active/inactive schedules (slotActiveNow). Default "Asia/Shanghai".
	// NOTE: billing-day boundaries (todayDayStr) use a separate, always-Shanghai
	// constant and are not controlled by this field (different semantic scope).
	QuietHoursTZ string

	// QuotaLimitDefaultResetMs is the fallback reset duration (ms from now) when
	// a quota-limit keyword is matched but no explicit reset time is parseable
	// (e.g. CPA "cooling down" messages). Default 300000 (5 minutes).
	QuotaLimitDefaultResetMs int64

	// UpstreamTimeoutSec is the total HTTP client timeout for upstream requests
	// (both node and fallback channel proxies). Default 300 (5 minutes).
	UpstreamTimeoutSec int

	// SpendCap5hEnabled enables 5-hour spend-cap enforcement. Default false.
	SpendCap5hEnabled bool
	// SpendCap5hUsd is the 5-hour spend cap range (per account). Default {Min: 100, Max: 200}.
	SpendCap5hUsd RangeF
	// SpendCap7dEnabled enables 7-day spend-cap enforcement. Default false.
	SpendCap7dEnabled bool
	// SpendCap7dUsd is the 7-day spend cap range (per account). Default {Min: 500, Max: 1000}.
	SpendCap7dUsd RangeF
	// SpendWindow5hMs is the 5-hour window duration in milliseconds. Default 18000000 (5 hours).
	SpendWindow5hMs int64
	// SpendWindow7dMs is the 7-day window duration in milliseconds. Default 604800000 (7 days).
	SpendWindow7dMs int64

	// HumanDelayDist selects the inter-slot cooldown distribution.
	// "uniform" (default) uses SlotCooldownMinMs/MaxMs for a uniform random delay.
	// "lognormal" uses HumanDelayP50Ms/HumanDelayP95Ms to parameterize a log-normal
	// distribution that produces human-like timing (most delays short, occasional long).
	HumanDelayDist string
	// HumanDelayP50Ms is the per-account 50th-percentile cooldown (ms) for log-normal mode.
	// Resolved deterministically per account. Default {Min: 2000, Max: 2000}.
	HumanDelayP50Ms RangeI
	// HumanDelayP95Ms is the per-account 95th-percentile cooldown (ms) for log-normal mode.
	// Resolved deterministically per account. Default {Min: 5000, Max: 5000}.
	HumanDelayP95Ms RangeI
}

// Defaults returns sane baseline configuration.
func Defaults() Config {
	return Config{
		MaxConcurrent:             3,
		SlotCooldownMinMs:         2000,
		SlotCooldownMaxMs:         5000,
		BanPersistStreak:          3,
		PermanentBanStreak:        5,
		CooldownBaseMs:            10000,
		CooldownMaxMs:             600000,
		CooldownMult:              2,
		AffinityTTLSec:            300,
		FallbackEnabled:           false,
		FallbackPriceThresholdUsd: 0.005,
		FallbackKeywords:          nil,
		FallbackModels:            nil,
		FallbackProbeEnabled:      false,
		BanSignals:                []int{401},
		BanKeywords:               []string{"authentication_error", "account_disabled", "account_suspended"},
		CooldownSignals:           nil,
		CooldownSignalSec:         60,
		MaxFailover:               50,
		WarmupHours:               0,
		WarmupMaxConcurrent:       1,
		WarmupBlockOpus:           true,
		ElasticEnabled:            false,
		ElasticBaselineCount:      1,
		ElasticScaleUpUtil:        0.8,
		ElasticScaleDownUtil:      0.3,
		ElasticMaxReserve:         1000,
		SessionErrorThreshold:     0,
		SessionCooldownSec:        300,
		ResponseExileEnabled:      false,
		ResponseExileKeywords:     []string{"usage policy", "i can't help with that request"},
		StreamIdleTimeoutSec:      120,
		// Precise limit phrases — NOT the bare "rate_limit_error" (transient). Covers
		// the subscription wording ("hit your limit" / "usage limit") AND CPA's
		// ("All credentials for model … are cooling down via provider claude").
		QuotaLimitKeywords: []string{"hit your limit", "usage limit", "cooling down"},
		// Quota limits arrive as 429 (CPA cooling-down) or 500 (meridian / in-body errors).
		QuotaLimitStatusCodes: []int{429, 500},
		// Official Anthropic per-model output ceilings (max_tokens). Editable per
		// tenant via the policy patch; an over-limit request is rejected 400 without
		// retry (limits-1).
		QuietHoursTZ:             "Asia/Shanghai",
		QuotaLimitDefaultResetMs: 300000,
		UpstreamTimeoutSec:       300,
		ModelMaxTokens: map[string]int{
			"claude-opus-4-8":   128000,
			"claude-opus-4-7":   128000,
			"claude-sonnet-4-6": 64000,
			"claude-haiku-4-5":  64000,
		},
		SpendCap5hEnabled: false,
		SpendCap5hUsd:     RangeF{Min: 100, Max: 200},
		SpendCap7dEnabled: false,
		SpendCap7dUsd:     RangeF{Min: 500, Max: 1000},
		SpendWindow5hMs:   18000000,
		SpendWindow7dMs:   604800000,
		HumanDelayDist:    "uniform",
		HumanDelayP50Ms:   RangeI{Min: 2000, Max: 2000},
		HumanDelayP95Ms:   RangeI{Min: 5000, Max: 5000},
	}
}

// Patch is one layer's partial override; nil fields are left unchanged.
type Patch struct {
	MaxConcurrent             *int
	SlotCooldownMinMs         *int64
	SlotCooldownMaxMs         *int64
	BanPersistStreak          *int
	PermanentBanStreak        *int
	CooldownBaseMs            *int64
	CooldownMaxMs             *int64
	CooldownMult              *float64
	AffinityTTLSec            *int
	FallbackEnabled           *bool
	FallbackPriceThresholdUsd *float64
	FallbackKeywords          *[]string
	FallbackModels            *[]string
	FallbackProbeEnabled      *bool
	BanSignals                *[]int
	BanKeywords               *[]string
	CooldownSignals           *[]int
	CooldownSignalSec         *int
	MaxFailover               *int
	WarmupHours               *int
	WarmupMaxConcurrent       *int
	WarmupBlockOpus           *bool
	ElasticEnabled            *bool
	ElasticBaselineCount      *int
	ElasticScaleUpUtil        *float64
	ElasticScaleDownUtil      *float64
	ElasticMaxReserve         *int
	SessionErrorThreshold     *int
	SessionCooldownSec        *int
	ResponseExileEnabled      *bool
	ResponseExileKeywords     *[]string
	StreamIdleTimeoutSec      *int
	QuotaLimitKeywords        *[]string
	QuotaLimitStatusCodes     *[]int
	ModelMaxTokens            *map[string]int
	QuietHoursTZ              *string
	QuotaLimitDefaultResetMs  *int64
	UpstreamTimeoutSec        *int
	SpendCap5hEnabled         *bool
	SpendCap5hUsd             *RangeF
	SpendCap7dEnabled         *bool
	SpendCap7dUsd             *RangeF
	SpendWindow5hMs           *int64
	SpendWindow7dMs           *int64
	HumanDelayDist            *string
	HumanDelayP50Ms           *RangeI
	HumanDelayP95Ms           *RangeI
}

func apply(c *Config, p Patch) {
	if p.MaxConcurrent != nil {
		c.MaxConcurrent = *p.MaxConcurrent
	}
	if p.SlotCooldownMinMs != nil {
		c.SlotCooldownMinMs = *p.SlotCooldownMinMs
	}
	if p.SlotCooldownMaxMs != nil {
		c.SlotCooldownMaxMs = *p.SlotCooldownMaxMs
	}
	if p.BanPersistStreak != nil {
		c.BanPersistStreak = *p.BanPersistStreak
	}
	if p.PermanentBanStreak != nil {
		c.PermanentBanStreak = *p.PermanentBanStreak
	}
	if p.CooldownBaseMs != nil {
		c.CooldownBaseMs = *p.CooldownBaseMs
	}
	if p.CooldownMaxMs != nil {
		c.CooldownMaxMs = *p.CooldownMaxMs
	}
	if p.CooldownMult != nil {
		c.CooldownMult = *p.CooldownMult
	}
	if p.AffinityTTLSec != nil {
		c.AffinityTTLSec = *p.AffinityTTLSec
	}
	if p.FallbackEnabled != nil {
		c.FallbackEnabled = *p.FallbackEnabled
	}
	if p.FallbackPriceThresholdUsd != nil {
		c.FallbackPriceThresholdUsd = *p.FallbackPriceThresholdUsd
	}
	if p.FallbackKeywords != nil {
		c.FallbackKeywords = *p.FallbackKeywords
	}
	if p.FallbackModels != nil {
		c.FallbackModels = *p.FallbackModels
	}
	if p.FallbackProbeEnabled != nil {
		c.FallbackProbeEnabled = *p.FallbackProbeEnabled
	}
	if p.BanSignals != nil {
		c.BanSignals = *p.BanSignals
	}
	if p.BanKeywords != nil {
		c.BanKeywords = *p.BanKeywords
	}
	if p.CooldownSignals != nil {
		c.CooldownSignals = *p.CooldownSignals
	}
	if p.CooldownSignalSec != nil {
		c.CooldownSignalSec = *p.CooldownSignalSec
	}
	if p.MaxFailover != nil {
		c.MaxFailover = *p.MaxFailover
	}
	if p.WarmupHours != nil {
		c.WarmupHours = *p.WarmupHours
	}
	if p.WarmupMaxConcurrent != nil {
		c.WarmupMaxConcurrent = *p.WarmupMaxConcurrent
	}
	if p.WarmupBlockOpus != nil {
		c.WarmupBlockOpus = *p.WarmupBlockOpus
	}
	if p.ElasticEnabled != nil {
		c.ElasticEnabled = *p.ElasticEnabled
	}
	if p.ElasticBaselineCount != nil {
		c.ElasticBaselineCount = *p.ElasticBaselineCount
	}
	if p.ElasticScaleUpUtil != nil {
		c.ElasticScaleUpUtil = *p.ElasticScaleUpUtil
	}
	if p.ElasticScaleDownUtil != nil {
		c.ElasticScaleDownUtil = *p.ElasticScaleDownUtil
	}
	if p.ElasticMaxReserve != nil {
		c.ElasticMaxReserve = *p.ElasticMaxReserve
	}
	if p.SessionErrorThreshold != nil {
		c.SessionErrorThreshold = *p.SessionErrorThreshold
	}
	if p.SessionCooldownSec != nil {
		c.SessionCooldownSec = *p.SessionCooldownSec
	}
	if p.ResponseExileEnabled != nil {
		c.ResponseExileEnabled = *p.ResponseExileEnabled
	}
	if p.ResponseExileKeywords != nil {
		c.ResponseExileKeywords = *p.ResponseExileKeywords
	}
	if p.StreamIdleTimeoutSec != nil {
		c.StreamIdleTimeoutSec = *p.StreamIdleTimeoutSec
	}
	if p.QuotaLimitKeywords != nil {
		c.QuotaLimitKeywords = *p.QuotaLimitKeywords
	}
	if p.QuotaLimitStatusCodes != nil {
		c.QuotaLimitStatusCodes = *p.QuotaLimitStatusCodes
	}
	if p.ModelMaxTokens != nil {
		c.ModelMaxTokens = *p.ModelMaxTokens
	}
	if p.QuietHoursTZ != nil {
		c.QuietHoursTZ = *p.QuietHoursTZ
	}
	if p.QuotaLimitDefaultResetMs != nil {
		c.QuotaLimitDefaultResetMs = *p.QuotaLimitDefaultResetMs
	}
	if p.UpstreamTimeoutSec != nil {
		c.UpstreamTimeoutSec = *p.UpstreamTimeoutSec
	}
	if p.SpendCap5hEnabled != nil {
		c.SpendCap5hEnabled = *p.SpendCap5hEnabled
	}
	if p.SpendCap5hUsd != nil {
		c.SpendCap5hUsd = *p.SpendCap5hUsd
	}
	if p.SpendCap7dEnabled != nil {
		c.SpendCap7dEnabled = *p.SpendCap7dEnabled
	}
	if p.SpendCap7dUsd != nil {
		c.SpendCap7dUsd = *p.SpendCap7dUsd
	}
	if p.SpendWindow5hMs != nil {
		c.SpendWindow5hMs = *p.SpendWindow5hMs
	}
	if p.SpendWindow7dMs != nil {
		c.SpendWindow7dMs = *p.SpendWindow7dMs
	}
	if p.HumanDelayDist != nil {
		c.HumanDelayDist = *p.HumanDelayDist
	}
	if p.HumanDelayP50Ms != nil {
		c.HumanDelayP50Ms = *p.HumanDelayP50Ms
	}
	if p.HumanDelayP95Ms != nil {
		c.HumanDelayP95Ms = *p.HumanDelayP95Ms
	}
}

// MaxTokensFor returns the configured output-token ceiling for model, matching the
// longest ModelMaxTokens key that is a substring of model (case-insensitive), or 0
// when no key matches (unlimited). Tolerates dated suffixes like
// "claude-haiku-4-5-20251001" matching the "claude-haiku-4-5" key.
func (c Config) MaxTokensFor(model string) int {
	m := strings.ToLower(model)
	best, bestLen := 0, -1
	for k, v := range c.ModelMaxTokens {
		lk := strings.ToLower(k)
		if strings.Contains(m, lk) && len(lk) > bestLen {
			best, bestLen = v, len(lk)
		}
	}
	return best
}

// Resolve applies patches in order onto base (later patches win).
func Resolve(base Config, patches ...Patch) Config {
	c := base
	for _, p := range patches {
		apply(&c, p)
	}
	return c
}

// Diff is a single field-level change between two configs.
type Diff struct {
	Field string
	From  string
	To    string
}

// DryRun resolves patches and reports field-level diffs relative to base.
func DryRun(base Config, patches ...Patch) (Config, []Diff) {
	final := Resolve(base, patches...)
	var diffs []Diff
	add := func(field string, from, to any) {
		fs, ts := fmt.Sprintf("%v", from), fmt.Sprintf("%v", to)
		if fs != ts {
			diffs = append(diffs, Diff{Field: field, From: fs, To: ts})
		}
	}
	add("MaxConcurrent", base.MaxConcurrent, final.MaxConcurrent)
	add("SlotCooldownMinMs", base.SlotCooldownMinMs, final.SlotCooldownMinMs)
	add("SlotCooldownMaxMs", base.SlotCooldownMaxMs, final.SlotCooldownMaxMs)
	add("BanPersistStreak", base.BanPersistStreak, final.BanPersistStreak)
	add("PermanentBanStreak", base.PermanentBanStreak, final.PermanentBanStreak)
	add("CooldownBaseMs", base.CooldownBaseMs, final.CooldownBaseMs)
	add("CooldownMaxMs", base.CooldownMaxMs, final.CooldownMaxMs)
	add("CooldownMult", base.CooldownMult, final.CooldownMult)
	add("AffinityTTLSec", base.AffinityTTLSec, final.AffinityTTLSec)
	add("FallbackEnabled", base.FallbackEnabled, final.FallbackEnabled)
	add("FallbackPriceThresholdUsd", base.FallbackPriceThresholdUsd, final.FallbackPriceThresholdUsd)
	add("FallbackKeywords", base.FallbackKeywords, final.FallbackKeywords)
	add("FallbackModels", base.FallbackModels, final.FallbackModels)
	add("FallbackProbeEnabled", base.FallbackProbeEnabled, final.FallbackProbeEnabled)
	add("BanSignals", base.BanSignals, final.BanSignals)
	add("BanKeywords", base.BanKeywords, final.BanKeywords)
	add("CooldownSignals", base.CooldownSignals, final.CooldownSignals)
	add("CooldownSignalSec", base.CooldownSignalSec, final.CooldownSignalSec)
	add("MaxFailover", base.MaxFailover, final.MaxFailover)
	add("WarmupHours", base.WarmupHours, final.WarmupHours)
	add("WarmupMaxConcurrent", base.WarmupMaxConcurrent, final.WarmupMaxConcurrent)
	add("WarmupBlockOpus", base.WarmupBlockOpus, final.WarmupBlockOpus)
	add("ElasticEnabled", base.ElasticEnabled, final.ElasticEnabled)
	add("ElasticBaselineCount", base.ElasticBaselineCount, final.ElasticBaselineCount)
	add("ElasticScaleUpUtil", base.ElasticScaleUpUtil, final.ElasticScaleUpUtil)
	add("ElasticScaleDownUtil", base.ElasticScaleDownUtil, final.ElasticScaleDownUtil)
	add("ElasticMaxReserve", base.ElasticMaxReserve, final.ElasticMaxReserve)
	add("SessionErrorThreshold", base.SessionErrorThreshold, final.SessionErrorThreshold)
	add("SessionCooldownSec", base.SessionCooldownSec, final.SessionCooldownSec)
	add("ResponseExileEnabled", base.ResponseExileEnabled, final.ResponseExileEnabled)
	add("ResponseExileKeywords", base.ResponseExileKeywords, final.ResponseExileKeywords)
	add("StreamIdleTimeoutSec", base.StreamIdleTimeoutSec, final.StreamIdleTimeoutSec)
	add("QuietHoursTZ", base.QuietHoursTZ, final.QuietHoursTZ)
	add("QuotaLimitDefaultResetMs", base.QuotaLimitDefaultResetMs, final.QuotaLimitDefaultResetMs)
	add("UpstreamTimeoutSec", base.UpstreamTimeoutSec, final.UpstreamTimeoutSec)
	return final, diffs
}
