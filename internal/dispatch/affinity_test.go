package dispatch

import (
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/session"
)

func TestApplyAffinity_MovesStickyToFront(t *testing.T) {
	svc := &Service{sess: session.NewStore(), Now: func() int64 { return 1000 }}
	order := []string{"nA:default", "nB:default", "nC:default"}

	// No affinity → unchanged.
	if got := svc.applyAffinity("conv", order, 1000); got[0] != "nA:default" {
		t.Fatalf("without affinity, order[0]=%s want nA:default", got[0])
	}

	// Set affinity to nC → it moves to front, others preserve relative order.
	svc.sess.SetAffinity("conv", "nC:default", 5000, 1000)
	got := svc.applyAffinity("conv", order, 2000)
	want := []string{"nC:default", "nA:default", "nB:default"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reordered=%v want %v", got, want)
		}
	}

	// Expired affinity → unchanged.
	if got := svc.applyAffinity("conv", order, 999999); got[0] != "nA:default" {
		t.Fatalf("expired affinity should not reorder, got[0]=%s", got[0])
	}
}
