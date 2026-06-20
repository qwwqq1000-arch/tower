package billing

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

// ComputeHostingFee returns the unsettled and accumulated hosting fees.
func ComputeHostingFee(consumption, settled, rate float64) (unsettled, accumulated float64) {
	u := consumption - settled
	if u < 0 {
		u = 0
	}
	return u * rate, consumption * rate
}
