// Package policy resolves the three-layer 封控 configuration (global → group/tenant
// → node/account) and produces dry-run diffs for the UI.
package policy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TimeWindow is a [StartMin, EndMin) interval expressed in minute-of-day (0–1439).
// Overnight windows have StartMin > EndMin (e.g. {1260, 240} = 21:00–04:00).
type TimeWindow struct {
	StartMin int `json:"StartMin"`
	EndMin   int `json:"EndMin"`
}

// InAnyWindow reports whether minuteOfDay falls inside any of the given windows.
// For a normal window (start <= end): active when start <= cur < end.
// For an overnight window (start > end): active when cur >= start OR cur < end.
func InAnyWindow(minuteOfDay int, windows []TimeWindow) bool {
	for _, w := range windows {
		if w.StartMin <= w.EndMin {
			if minuteOfDay >= w.StartMin && minuteOfDay < w.EndMin {
				return true
			}
		} else {
			// overnight: e.g. 21:00 → 04:00
			if minuteOfDay >= w.StartMin || minuteOfDay < w.EndMin {
				return true
			}
		}
	}
	return false
}

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
	AffinityWaitMs            int
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
	// HumanDelayEnabled gates the human-delay feature. When false (default), the
	// effective distribution is always "uniform" regardless of HumanDelayDist.
	// When true, HumanDelayDist applies.
	HumanDelayEnabled bool
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

	// RateGovEnabled enables per-account RPM/RPH/RPD rate limiting in dispatch.
	// Default false (zero overhead when disabled).
	RateGovEnabled bool
	// RateRPM is the per-account requests-per-minute limit (resolved per account).
	// Default {8, 8}.
	RateRPM RangeI
	// RateRPH is the per-account requests-per-hour limit (resolved per account).
	// Default {100, 100}.
	RateRPH RangeI
	// RateRPD is the per-account requests-per-day limit (resolved per account).
	// Default {600, 600}.
	RateRPD RangeI
	// RateExceedAction determines what happens when a rate limit is exceeded:
	// only "rotate" (default) is supported; "delay" was removed (see fix-b-report.md).
	RateExceedAction string

	// SessionSimEnabled enables session-simulation burst→pause rotation.
	// When true, each account counts consecutive successful requests; once the
	// per-account burst target is reached the account is temporarily rotated out
	// (via SetLimited "all") for a randomised pause, then recovers automatically.
	// Default false (zero overhead when disabled).
	SessionSimEnabled bool
	// SessionBurstCount is the per-account burst-size target range (number of
	// consecutive successful requests before a coffee-break pause). Resolved
	// deterministically per account. Default {3, 10}.
	SessionBurstCount RangeI
	// SessionPauseMs is the pause duration range (ms) applied when an account
	// completes a burst. Resolved deterministically per account. Default {30000, 180000}.
	SessionPauseMs RangeI

	// ModelPinEnabled enables per-account model pinning (disguise-phase4). Default false.
	// When enabled, each account is restricted to serving a single model class to avoid
	// the multi-model-simultaneously ban signal.
	ModelPinEnabled bool
	// ModelPinMode selects the pinning strategy: "sticky" (default) pins the account to
	// the first model it serves (TTL = AffinityTTLSec); "fixed" restricts the account to
	// ModelPinTarget only (regardless of TTL).
	ModelPinMode string
	// ModelPinTarget is the model substring used in "fixed" mode. Accounts only enter the
	// candidate pool when the request model matches this target (case-insensitive substring).
	// Empty string = no restriction (effectively disables fixed-mode filtering).
	ModelPinTarget string

	// SerialQueueEnabled forces each account to serve at most 1 request at a time
	// (concurrency=1). Default false (zero overhead when disabled).
	// When combined with QuietHours, effectiveCap = min(1, QuietHoursConcurrency).
	// SerialQueueWaitMs is reserved for future bounded-wait behaviour; not yet active.
	SerialQueueEnabled bool
	// SerialQueueWaitMs is the maximum time (ms) to wait for a serial slot before
	// failing over to the next candidate. Default 2000. Currently a TODO placeholder —
	// the wait-for-slot logic is not yet implemented; only the capacity cap (=1) is active.
	SerialQueueWaitMs int

	// IdleFirstSelection orders candidates by current inflight (least-busy first) with
	// a random tiebreak so equally-idle accounts share load evenly. Without this, the
	// stable weight-desc sort causes order[0] to receive all traffic when weights are
	// equal and MaxFailover=1. Default true (fixes the deterministic-concentration bug).
	IdleFirstSelection bool

	// DirectFallbackStatusCodes: when a node response has an HTTP status in this list
	// AND the body contains any DirectFallbackKeywords substring, dispatch stops trying
	// further accounts and falls straight through to the fallback channel (skipping the
	// rest of the pool). Rationale: a concurrency-limit response means every account
	// shares the same limit — hammering the rest of the pool wastes attempts.
	// Default [400]. Empty = feature off.
	DirectFallbackStatusCodes []int
	// DirectFallbackKeywords are case-insensitive body substrings that, combined with
	// a matching DirectFallbackStatusCodes entry, trigger immediate fallback routing.
	// Default ["rate_limit_error"]. Empty = feature off.
	DirectFallbackKeywords []string

	// RetryDelayMs is the delay (ms) inserted between failover attempts (both between
	// different accounts and between same-account retries). Default 0 = no delay.
	RetryDelayMs int
	// RetrySameAccountMax is the number of ADDITIONAL attempts on the SAME account
	// before moving to the next (0 = move on immediately after first failure, the
	// original behaviour). Each same-account retry also respects RetryDelayMs.
	RetrySameAccountMax int

	// BodyPadEnabled enables request-body padding (disguise-phase4). Default false.
	// When true and BodyPadBytes resolves to > 0, a deterministic pad string is
	// injected into the metadata.pad field of each request before dispatch.
	// Injection vector: metadata object (Anthropic API accepts arbitrary metadata keys).
	// SAFETY: verify the injection vector against real upstream before enabling —
	// see task brief Step 1. Default false + BodyPadBytes={0,0} means the feature
	// is fully dormant even if someone flips BodyPadEnabled=true without also
	// configuring a non-zero BodyPadBytes range.
	BodyPadEnabled bool
	// BodyPadBytes is the per-request pad size range (bytes). Resolved deterministically
	// per conversation seed. Default {0,0} (no padding). Even when BodyPadEnabled=true,
	// a {0,0} range means padBody is called with nBytes=0, which is a no-op.
	BodyPadBytes RangeI

	// QuietHoursEnabled enables quiet-hours rate/concurrency reduction. Default false.
	QuietHoursEnabled bool
	// QuietHoursWindows defines the quiet time windows (minute-of-day, supports overnight).
	// Default [{1260, 240}] = 21:00–04:00. Uses QuietHoursTZ for timezone.
	QuietHoursWindows []TimeWindow
	// QuietHoursRPM is the per-account RPM cap applied during quiet hours.
	// Default {1, 2}. Resolved deterministically per account.
	QuietHoursRPM RangeI
	// QuietHoursConcurrency is the effective max-concurrent cap during quiet hours.
	// Default 1. Applied via SetCapacity; takes min with MaxConcurrent.
	QuietHoursConcurrency int

	// QuotaFullThreshold is the utilization fraction (0–100 scale matching CPA usage API)
	// above which a window is considered exhausted for reactive quota classification.
	// Default 99.0.
	QuotaFullThreshold float64
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
		AffinityWaitMs:            2000,
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
		HumanDelayEnabled: false,
		HumanDelayDist:    "uniform",
		HumanDelayP50Ms:   RangeI{Min: 2000, Max: 2000},
		HumanDelayP95Ms:   RangeI{Min: 5000, Max: 5000},
		RateGovEnabled:    false,
		RateRPM:           RangeI{Min: 8, Max: 8},
		RateRPH:           RangeI{Min: 100, Max: 100},
		RateRPD:           RangeI{Min: 600, Max: 600},
		RateExceedAction:  "rotate",
		SessionSimEnabled: false,
		SessionBurstCount: RangeI{Min: 3, Max: 10},
		SessionPauseMs:    RangeI{Min: 30000, Max: 180000},
		// QuietHours: off by default; window = 21:00–04:00 (Asia/Shanghai via QuietHoursTZ).
		QuietHoursEnabled:     false,
		QuietHoursWindows:     []TimeWindow{{StartMin: 1260, EndMin: 240}},
		QuietHoursRPM:         RangeI{Min: 1, Max: 2},
		QuietHoursConcurrency: 1,
		ModelPinEnabled:       false,
		ModelPinMode:          "sticky",
		ModelPinTarget:        "",
		SerialQueueEnabled:    false,
		SerialQueueWaitMs:     2000,
		BodyPadEnabled:        false,
		BodyPadBytes:          RangeI{Min: 0, Max: 0},
		IdleFirstSelection:    true,
		// DirectFallback defaults: ON for the concurrency-limit storm case.
		// A 400+rate_limit_error from a node means all accounts share the same limit;
		// failing over hammers them all. Codes/keywords both non-empty → feature active.
		DirectFallbackStatusCodes: []int{400},
		DirectFallbackKeywords:    []string{"rate_limit_error"},
		RetryDelayMs:              0,
		RetrySameAccountMax:       0,
		QuotaFullThreshold:        99.0,
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
	AffinityWaitMs            *int
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
	HumanDelayEnabled         *bool
	HumanDelayDist            *string
	HumanDelayP50Ms           *RangeI
	HumanDelayP95Ms           *RangeI
	RateGovEnabled            *bool
	RateRPM                   *RangeI
	RateRPH                   *RangeI
	RateRPD                   *RangeI
	RateExceedAction          *string
	SessionSimEnabled         *bool
	SessionBurstCount         *RangeI
	SessionPauseMs            *RangeI

	QuietHoursEnabled     *bool
	QuietHoursWindows     *[]TimeWindow
	QuietHoursRPM         *RangeI
	QuietHoursConcurrency *int

	ModelPinEnabled *bool
	ModelPinMode    *string
	ModelPinTarget  *string

	SerialQueueEnabled *bool
	SerialQueueWaitMs  *int

	BodyPadEnabled *bool
	BodyPadBytes   *RangeI

	IdleFirstSelection *bool

	DirectFallbackStatusCodes *[]int
	DirectFallbackKeywords    *[]string
	RetryDelayMs              *int
	RetrySameAccountMax       *int

	QuotaFullThreshold *float64
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
	if p.AffinityWaitMs != nil {
		c.AffinityWaitMs = *p.AffinityWaitMs
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
	if p.HumanDelayEnabled != nil {
		c.HumanDelayEnabled = *p.HumanDelayEnabled
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
	if p.RateGovEnabled != nil {
		c.RateGovEnabled = *p.RateGovEnabled
	}
	if p.RateRPM != nil {
		c.RateRPM = *p.RateRPM
	}
	if p.RateRPH != nil {
		c.RateRPH = *p.RateRPH
	}
	if p.RateRPD != nil {
		c.RateRPD = *p.RateRPD
	}
	if p.RateExceedAction != nil {
		c.RateExceedAction = *p.RateExceedAction
	}
	if p.SessionSimEnabled != nil {
		c.SessionSimEnabled = *p.SessionSimEnabled
	}
	if p.SessionBurstCount != nil {
		c.SessionBurstCount = *p.SessionBurstCount
	}
	if p.SessionPauseMs != nil {
		c.SessionPauseMs = *p.SessionPauseMs
	}
	if p.QuietHoursEnabled != nil {
		c.QuietHoursEnabled = *p.QuietHoursEnabled
	}
	if p.QuietHoursWindows != nil {
		c.QuietHoursWindows = *p.QuietHoursWindows
	}
	if p.QuietHoursRPM != nil {
		c.QuietHoursRPM = *p.QuietHoursRPM
	}
	if p.QuietHoursConcurrency != nil {
		c.QuietHoursConcurrency = *p.QuietHoursConcurrency
	}
	if p.ModelPinEnabled != nil {
		c.ModelPinEnabled = *p.ModelPinEnabled
	}
	if p.ModelPinMode != nil {
		c.ModelPinMode = *p.ModelPinMode
	}
	if p.ModelPinTarget != nil {
		c.ModelPinTarget = *p.ModelPinTarget
	}
	if p.SerialQueueEnabled != nil {
		c.SerialQueueEnabled = *p.SerialQueueEnabled
	}
	if p.SerialQueueWaitMs != nil {
		c.SerialQueueWaitMs = *p.SerialQueueWaitMs
	}
	if p.BodyPadEnabled != nil {
		c.BodyPadEnabled = *p.BodyPadEnabled
	}
	if p.BodyPadBytes != nil {
		c.BodyPadBytes = *p.BodyPadBytes
	}
	if p.IdleFirstSelection != nil {
		c.IdleFirstSelection = *p.IdleFirstSelection
	}
	if p.DirectFallbackStatusCodes != nil {
		c.DirectFallbackStatusCodes = *p.DirectFallbackStatusCodes
	}
	if p.DirectFallbackKeywords != nil {
		c.DirectFallbackKeywords = *p.DirectFallbackKeywords
	}
	if p.RetryDelayMs != nil {
		c.RetryDelayMs = *p.RetryDelayMs
	}
	if p.RetrySameAccountMax != nil {
		c.RetrySameAccountMax = *p.RetrySameAccountMax
	}
	if p.QuotaFullThreshold != nil {
		c.QuotaFullThreshold = *p.QuotaFullThreshold
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
	add("AffinityWaitMs", base.AffinityWaitMs, final.AffinityWaitMs)
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
	// Phase 2: SpendCap
	add("SpendCap5hEnabled", base.SpendCap5hEnabled, final.SpendCap5hEnabled)
	add("SpendCap5hUsd", base.SpendCap5hUsd, final.SpendCap5hUsd)
	add("SpendCap7dEnabled", base.SpendCap7dEnabled, final.SpendCap7dEnabled)
	add("SpendCap7dUsd", base.SpendCap7dUsd, final.SpendCap7dUsd)
	// Phase 3: HumanDelay
	add("HumanDelayEnabled", base.HumanDelayEnabled, final.HumanDelayEnabled)
	add("HumanDelayDist", base.HumanDelayDist, final.HumanDelayDist)
	add("HumanDelayP50Ms", base.HumanDelayP50Ms, final.HumanDelayP50Ms)
	add("HumanDelayP95Ms", base.HumanDelayP95Ms, final.HumanDelayP95Ms)
	// Phase 3: RateGovernor
	add("RateGovEnabled", base.RateGovEnabled, final.RateGovEnabled)
	add("RateRPM", base.RateRPM, final.RateRPM)
	add("RateRPH", base.RateRPH, final.RateRPH)
	add("RateRPD", base.RateRPD, final.RateRPD)
	add("RateExceedAction", base.RateExceedAction, final.RateExceedAction)
	// Phase 3: SessionSim
	add("SessionSimEnabled", base.SessionSimEnabled, final.SessionSimEnabled)
	add("SessionBurstCount", base.SessionBurstCount, final.SessionBurstCount)
	add("SessionPauseMs", base.SessionPauseMs, final.SessionPauseMs)
	// Phase 3: QuietHours
	add("QuietHoursEnabled", base.QuietHoursEnabled, final.QuietHoursEnabled)
	add("QuietHoursWindows", base.QuietHoursWindows, final.QuietHoursWindows)
	add("QuietHoursRPM", base.QuietHoursRPM, final.QuietHoursRPM)
	add("QuietHoursConcurrency", base.QuietHoursConcurrency, final.QuietHoursConcurrency)
	// Phase 4: ModelPin
	add("ModelPinEnabled", base.ModelPinEnabled, final.ModelPinEnabled)
	add("ModelPinMode", base.ModelPinMode, final.ModelPinMode)
	add("ModelPinTarget", base.ModelPinTarget, final.ModelPinTarget)
	// Phase 4: SerialQueue
	add("SerialQueueEnabled", base.SerialQueueEnabled, final.SerialQueueEnabled)
	add("SerialQueueWaitMs", base.SerialQueueWaitMs, final.SerialQueueWaitMs)
	// Phase 4: BodyPad
	add("BodyPadEnabled", base.BodyPadEnabled, final.BodyPadEnabled)
	add("BodyPadBytes", base.BodyPadBytes, final.BodyPadBytes)
	// Idle-first selection
	add("IdleFirstSelection", base.IdleFirstSelection, final.IdleFirstSelection)
	// Retry policy
	add("DirectFallbackStatusCodes", base.DirectFallbackStatusCodes, final.DirectFallbackStatusCodes)
	add("DirectFallbackKeywords", base.DirectFallbackKeywords, final.DirectFallbackKeywords)
	add("RetryDelayMs", base.RetryDelayMs, final.RetryDelayMs)
	add("RetrySameAccountMax", base.RetrySameAccountMax, final.RetrySameAccountMax)
	// Phase 5: reactive quota
	add("QuotaFullThreshold", base.QuotaFullThreshold, final.QuotaFullThreshold)
	return final, diffs
}
