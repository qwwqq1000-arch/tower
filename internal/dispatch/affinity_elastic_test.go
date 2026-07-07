package dispatch

import (
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/session"
)

// TestPinToAffinity_OverElastic verifies that pinToAffinity finds a pinned
// account even when it is NOT in the elastic-filtered order (reserve accounts).
// This tests the internal helper directly; the integration (allKeys threading)
// is verified by the buildCandidates return contract test.
func TestPinToAffinity_OverElastic(t *testing.T) {
	svc := &Service{sess: session.NewStore(), Now: func() int64 { return 1000 }}

	// Pinned to reserveKey, but elastic only exposes baseline.
	reserveKey := "nReserve:default"
	baselineOnly := []string{"nA:default", "nB:default"}
	allKeys := append(baselineOnly, reserveKey)

	// Pin to the reserve key.
	svc.sess.SetAffinity("conv1", reserveKey, 60000, 1000)

	// With only baseline order (elastic active, not scaled up) → no match → returns nil.
	if got := svc.pinToAffinity("conv1", baselineOnly, 2000); got != nil {
		t.Fatalf("baseline-only search should return nil for reserve-pinned conv, got %v", got)
	}

	// With allKeys (full candidate set) → should find the pinned reserve account.
	got := svc.pinToAffinity("conv1", allKeys, 2000)
	if len(got) != 1 || got[0] != reserveKey {
		t.Fatalf("allKeys search should find reserve-pinned account %q, got %v", reserveKey, got)
	}
}
