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

// CostUsd computes the USD cost of one request. Cache read = input×0.1,
// cache create(5m) = input×1.25 (Anthropic pricing).
func CostUsd(model string, inTok, outTok, cacheReadTok, cacheCreateTok int64) float64 {
	p := ModelPrice(model)
	total := float64(inTok)*p.InputPer1M +
		float64(outTok)*p.OutputPer1M +
		float64(cacheReadTok)*p.InputPer1M*0.1 +
		float64(cacheCreateTok)*p.InputPer1M*1.25
	return total / 1_000_000
}
