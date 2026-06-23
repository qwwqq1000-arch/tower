package billing

import "testing"

func TestApplyLedgerDelta_FirstObservationSetsBaseline(t *testing.T) {
	s := ApplyLedgerDelta(Ledger{Cum: 0, Last: -1}, 100)
	if s.Cum != 0 || s.Last != 100 {
		t.Fatalf("first: %+v, want cum=0 last=100", s)
	}
}

func TestApplyLedgerDelta_NormalIncrement(t *testing.T) {
	s := ApplyLedgerDelta(Ledger{Cum: 0, Last: 100}, 150)
	if s.Cum != 50 || s.Last != 150 {
		t.Fatalf("normal: %+v, want cum=50 last=150", s)
	}
}

func TestApplyLedgerDelta_NodeReset(t *testing.T) {
	// node restarted: cur (30) < last (150) → add full cur
	s := ApplyLedgerDelta(Ledger{Cum: 50, Last: 150}, 30)
	if s.Cum != 80 || s.Last != 30 {
		t.Fatalf("reset: %+v, want cum=80 last=30", s)
	}
}

func TestComputeHostingFee(t *testing.T) {
	// Fee-based: totalFee=150, settledFee=40 → unsettled=110, accumulated=150.
	uns, acc := ComputeHostingFee(150, 40)
	if uns != 110 {
		t.Fatalf("unsettled=%v, want 110", uns)
	}
	if acc != 150 {
		t.Fatalf("accumulated=%v, want 150", acc)
	}
	// settled exceeds total → unsettled clamps to 0
	if uns2, _ := ComputeHostingFee(50, 80); uns2 != 0 {
		t.Fatalf("unsettled=%v, want 0 (clamped)", uns2)
	}
}

func TestTotalHostingFee(t *testing.T) {
	// node 873.53 @ 30% + channel 6319.05 @ 0% = 262.059
	if got := TotalHostingFee(873.53, 0.30, 6319.05, 0.0); RoundUSD(got) != 262.06 {
		t.Fatalf("total fee=%v, want 262.06", RoundUSD(got))
	}
	if got := TotalHostingFee(100, 0.30, 200, 0.10); got != 50 { // 30 + 20
		t.Fatalf("total fee=%v, want 50", got)
	}
}
