// Package policy resolves the three-layer 封控 configuration (global → group/tenant
// → node/account) and produces dry-run diffs for the UI.
package policy

import "fmt"

// Config is the resolved 封控 configuration (representative core fields).
type Config struct {
	MaxConcurrent             int
	SlotCooldownMinMs         int64
	SlotCooldownMaxMs         int64
	BanPersistStreak          int
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
}

// Defaults returns sane baseline configuration.
func Defaults() Config {
	return Config{
		MaxConcurrent:             3,
		SlotCooldownMinMs:         2000,
		SlotCooldownMaxMs:         5000,
		BanPersistStreak:          3,
		CooldownBaseMs:            10000,
		CooldownMaxMs:             600000,
		CooldownMult:              2,
		AffinityTTLSec:            300,
		FallbackEnabled:           false,
		FallbackPriceThresholdUsd: 0.005,
		FallbackKeywords:          nil,
		FallbackModels:            nil,
		FallbackProbeEnabled:      false,
		BanSignals:                []int{401, 403},
		BanKeywords:               []string{"authentication_error", "account_disabled", "account_suspended"},
	}
}

// Patch is one layer's partial override; nil fields are left unchanged.
type Patch struct {
	MaxConcurrent             *int
	SlotCooldownMinMs         *int64
	SlotCooldownMaxMs         *int64
	BanPersistStreak          *int
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
	return final, diffs
}
