package dispatch

import (
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/session"
)

func TestPinToAffinity_Strict(t *testing.T) {
	// Strict session affinity (account-affinity-A): a pinned conversation is served
	// ONLY by its pinned account — never a second node account; if the pinned account
	// is unavailable the node list is emptied so it falls through to fallback.
	svc := &Service{sess: session.NewStore(), Now: func() int64 { return 1000 }}
	order := []string{"nA:default", "nB:default", "nC:default"}

	// No pin yet → full list unchanged (normal load-balanced selection).
	if got := svc.pinToAffinity("conv", order, 1000); len(got) != 3 {
		t.Fatalf("without affinity want full list, got %v", got)
	}

	// Pin to nC (present in candidates) → it is the SOLE candidate.
	svc.sess.SetAffinity("conv", "nC:default", 5000, 1000)
	got := svc.pinToAffinity("conv", order, 2000)
	if len(got) != 1 || got[0] != "nC:default" {
		t.Fatalf("strict pin should yield only [nC:default], got %v", got)
	}

	// Pinned account absent from candidates (rotated out/banned/gone) → nil → force
	// fallback, NOT a hop to another node account.
	if got := svc.pinToAffinity("conv", []string{"nA:default", "nB:default"}, 2000); got != nil {
		t.Fatalf("pinned-but-absent must force fallback (nil), got %v", got)
	}

	// Expired affinity → full list restored (re-selects, then re-pins).
	if got := svc.pinToAffinity("conv", order, 999999); len(got) != 3 {
		t.Fatalf("expired affinity should restore full list, got %v", got)
	}
}
