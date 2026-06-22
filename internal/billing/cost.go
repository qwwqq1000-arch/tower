// Package billing computes request cost and hosting fees, and is the source of
// truth for usage-based charges.
package billing

import "strings"

// Price is per-1M-token USD pricing for a model family.
type Price struct {
	InputPer1M  float64
	OutputPer1M float64
}

// ModelPrice returns pricing by model-family substring (defaults to sonnet tier).
func ModelPrice(model string) Price {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "opus"):
		return Price{InputPer1M: 5, OutputPer1M: 25}
	case strings.Contains(m, "haiku"):
		return Price{InputPer1M: 1, OutputPer1M: 5}
	default: // sonnet and unknown
		return Price{InputPer1M: 3, OutputPer1M: 15}
	}
}

// CostUsdFull computes cost incl. cache. read=input×0.1, write5m=input×1.25, write1h=input×2.0.
func CostUsdFull(model string, inTok, outTok, cacheReadTok, cache5mTok, cache1hTok int64) float64 {
	p := ModelPrice(model)
	total := float64(inTok)*p.InputPer1M +
		float64(outTok)*p.OutputPer1M +
		float64(cacheReadTok)*p.InputPer1M*0.1 +
		float64(cache5mTok)*p.InputPer1M*1.25 +
		float64(cache1hTok)*p.InputPer1M*2.0
	return total / 1_000_000
}

// CostUsd computes the USD cost of one request. Cache read = input×0.1,
// cache create(5m) = input×1.25 (Anthropic pricing).
// Treats cacheCreateTok as 5m cache writes (cache1h=0).
func CostUsd(model string, inTok, outTok, cacheReadTok, cacheCreateTok int64) float64 {
	return CostUsdFull(model, inTok, outTok, cacheReadTok, cacheCreateTok, 0)
}

// KnownModel reports whether the model maps to an explicit price family
// (opus/haiku/sonnet). Unknown models fall back to sonnet pricing in ModelPrice;
// callers should surface that fallback rather than bill silently.
func KnownModel(model string) bool {
	m := strings.ToLower(model)
	return strings.Contains(m, "opus") || strings.Contains(m, "haiku") || strings.Contains(m, "sonnet")
}
