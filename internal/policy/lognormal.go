package policy

import (
	"math"
	"math/rand"
)

// SampleLogNormal samples a log-normal distribution parameterized by p50 and p95,
// drawing a fresh random sample per call so each request gets its own delay.
//
// The log-normal parameters are derived as:
//
//	μ = ln(p50)
//	σ = (ln(p95) - μ) / z95    where z95 ≈ 1.6449 (the 95th percentile of the standard normal)
//
// The returned value is exp(μ + σ·Φ⁻¹(u)) where u ~ Uniform(0,1).
// This gives a distribution where the median equals p50 and the 95th percentile equals p95.
//
// Edge cases:
//   - p50 <= 0: returns 0
//   - p95 <= p50: returns p50 (degenerate / single point)
func SampleLogNormal(p50, p95 float64) float64 {
	if p50 <= 0 {
		return 0
	}
	if p95 <= p50 {
		return p50
	}
	mu := math.Log(p50)
	// z95 = Φ⁻¹(0.95) ≈ 1.6448536269514722
	const z95 = 1.6448536269514722
	sigma := (math.Log(p95) - mu) / z95

	u := rand.Float64() // fresh per-request sample
	if u == 0 {
		u = 1e-9 // guard: Erfinv(2*0-1) = Erfinv(-1) = -Inf
	}
	// Φ⁻¹(u) via math.Erfinv (Go 1.10+): Φ⁻¹(u) = √2 · erf⁻¹(2u−1)
	z := math.Sqrt2 * math.Erfinv(2*u-1)
	return math.Exp(mu + sigma*z)
}
