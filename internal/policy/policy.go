// Package policy resolves the three-layer 封控 configuration (global → group/tenant
// → node/account) and produces dry-run diffs for the UI.
package policy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PickThreshold extracts QuotaRotateThreshold from a JSON policy patch (the
// global policy row's params). If the patch is absent, unparseable, or holds an
// invalid value (<=0 or >1), it returns def unchanged. This is the single source
// of truth for how the meridian poller and the CPA discovery loop pick up the
// live QuotaRotateThreshold, so both account kinds gate on the same value.
func PickThreshold(patchJSON []byte, def float64) float64 {
	var p Patch
	if err := json.Unmarshal(patchJSON, &p); err != nil {
		return def
	}
	if p.QuotaRotateThreshold != nil {
		if v := *p.QuotaRotateThreshold; v > 0 && v <= 1 {
			return v
		}
	}
	return def
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
	CooldownSignals      []int
	CooldownSignalSec    int
	QuotaRotateThreshold float64
	MaxFailover          int

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

	// ModelMaxTokens caps the requested output tokens (the body's max_tokens) per
	// model. A request whose max_tokens exceeds the matched ceiling is rejected with
	// a 400 BEFORE any node/fallback attempt (no retry) — an over-limit request fails
	// on every upstream, so retrying wastes attempts (limits-1). Keys match a model
	// by longest substring (e.g. "claude-opus-4-8" matches "claude-opus-4-8" and any
	// dated suffix). Empty/zero ceiling = unlimited for that model.
	ModelMaxTokens map[string]int
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
		QuotaRotateThreshold:      0.95,
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
		// Official Anthropic per-model output ceilings (max_tokens). Editable per
		// tenant via the policy patch; an over-limit request is rejected 400 without
		// retry (limits-1).
		ModelMaxTokens: map[string]int{
			"claude-opus-4-8":   128000,
			"claude-opus-4-7":   128000,
			"claude-sonnet-4-6": 64000,
			"claude-haiku-4-5":  64000,
		},
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
	QuotaRotateThreshold      *float64
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
	ModelMaxTokens            *map[string]int
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
	if p.QuotaRotateThreshold != nil {
		if v := *p.QuotaRotateThreshold; v > 0 && v <= 1 {
			c.QuotaRotateThreshold = v
		} else {
			c.QuotaRotateThreshold = 0.95
		}
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
	if p.ModelMaxTokens != nil {
		c.ModelMaxTokens = *p.ModelMaxTokens
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
	add("QuotaRotateThreshold", base.QuotaRotateThreshold, final.QuotaRotateThreshold)
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
	return final, diffs
}
