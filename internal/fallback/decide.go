// Package fallback decides when a request routes to an external relay channel,
// and which channel.
package fallback

import (
	"strings"
	"unicode/utf8"
)

// Trigger names why a request was routed to fallback.
type Trigger string

const (
	None      Trigger = ""
	Keyword   Trigger = "keyword"
	Model     Trigger = "model"
	Probe     Trigger = "probe"
	Price     Trigger = "price"
	Exhausted Trigger = "exhausted"
)

// DecideInput carries everything needed to choose a routing trigger.
type DecideInput struct {
	Model             string
	BodyText          string
	ProbeText         string // extracted last user message text for probe detection
	EstCostUsd        float64
	PoolEmpty         bool
	Keywords          []string
	FallbackModels    []string
	PriceThresholdUsd float64
	ProbeEnabled      bool
}

var probeWords = map[string]bool{"hi": true, "ping": true, "测活": true, "hello": true, "test": true}

// IsProbe reports whether body looks like a health-check probe.
func IsProbe(body string) bool {
	t := strings.TrimSpace(body)
	if utf8.RuneCountInString(t) > 12 {
		return false
	}
	return probeWords[strings.ToLower(t)]
}

func containsAny(hay string, needles []string) bool {
	h := strings.ToLower(hay)
	for _, n := range needles {
		if n != "" && strings.Contains(h, strings.ToLower(n)) {
			return true
		}
	}
	return false
}

// EffectivePriceThreshold returns the per-channel price threshold when it is
// non-zero; otherwise it falls back to the global policy threshold. This lets
// each fallback channel specify its own routing sensitivity without forcing a
// global change.
func EffectivePriceThreshold(channelThreshold, globalThreshold float64) float64 {
	if channelThreshold > 0 {
		return channelThreshold
	}
	return globalThreshold
}

// Decide returns the highest-priority fallback trigger (None = use our nodes).
func Decide(in DecideInput) Trigger {
	if containsAny(in.BodyText, in.Keywords) {
		return Keyword
	}
	if containsAny(in.Model, in.FallbackModels) {
		return Model
	}
	if in.ProbeEnabled && IsProbe(in.ProbeText) {
		return Probe
	}
	if in.PriceThresholdUsd > 0 && in.EstCostUsd < in.PriceThresholdUsd {
		return Price
	}
	if in.PoolEmpty {
		return Exhausted
	}
	return None
}
