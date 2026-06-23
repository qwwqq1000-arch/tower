package billing

import "math"

// Ledger tracks cumulative consumption derived from a node's monotonic lifetime
// cost, robust to node restarts. Last < 0 means "no observation yet".
type Ledger struct {
	Cum  float64
	Last float64
}

// ApplyLedgerDelta folds a new lifetime-cost observation into the ledger.
func ApplyLedgerDelta(s Ledger, cur float64) Ledger {
	if s.Last < 0 {
		// first observation: establish baseline, no delta
		return Ledger{Cum: s.Cum, Last: cur}
	}
	if cur < s.Last {
		// node reset/restart: lifetime counter dropped → add full current value
		return Ledger{Cum: s.Cum + cur, Last: cur}
	}
	return Ledger{Cum: s.Cum + (cur - s.Last), Last: cur}
}

// ComputeHostingFee returns the unsettled and accumulated (total) hosting FEE.
// totalFee is the all-time accrued fee — node consumption×hostingRate PLUS channel
// relay consumption×channelRate. settledFee is the fee already settled. Billing now
// settles the FEE (托管费 + 渠道托管费), not raw consumption, so settlements track fee
// and the ledger shows the fee owed (billing-fee-1).
func ComputeHostingFee(totalFee, settledFee float64) (unsettled, accumulated float64) {
	u := totalFee - settledFee
	if u < 0 {
		u = 0
	}
	return u, totalFee
}

// TotalHostingFee is the all-time accrued hosting fee: node consumption at the
// hosting rate plus channel relay consumption at the channel rate.
func TotalHostingFee(consumption, rate, channelConsumption, channelRate float64) float64 {
	return consumption*rate + channelConsumption*channelRate
}

// OutstandingToSettle returns the not-yet-settled consumption (gross minus what
// was already settled), clamped to >= 0. Settling this amount is idempotent: a
// second settle right after sees gross == alreadySettled and settles 0, so a
// tenant is never double-charged for the same consumption.
func OutstandingToSettle(gross, alreadySettled float64) float64 {
	o := gross - alreadySettled
	if o < 0 {
		o = 0
	}
	return o
}

// RoundUSD rounds a USD amount to whole cents, removing float64 drift before a
// money value is shown to or charged to a tenant.
func RoundUSD(v float64) float64 {
	return math.Round(v*100) / 100
}
