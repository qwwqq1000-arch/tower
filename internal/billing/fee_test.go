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
	uns, acc := ComputeHostingFee(100, 40, 1.5)
	if uns != 90 { // (100-40)*1.5
		t.Fatalf("unsettled=%v, want 90", uns)
	}
	if acc != 150 { // 100*1.5
		t.Fatalf("accumulated=%v, want 150", acc)
	}
	// settled exceeds consumption → unsettled clamps to 0
	uns2, _ := ComputeHostingFee(50, 80, 2)
	if uns2 != 0 {
		t.Fatalf("unsettled=%v, want 0 (clamped)", uns2)
	}
}
