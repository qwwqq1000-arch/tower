package dispatch

import (
	"os"
	"strings"
	"testing"
)

// TestBothTiersConsultEnvelope is a mirror-guard that ensures both Dispatch
// and DispatchStream call missingEnvelopePieces — preventing tier drift where
// one tier is updated and the other is silently skipped.
func TestBothTiersConsultEnvelope(t *testing.T) {
	src, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatal(err)
	}
	// crude but effective tier-drift guard: detection must appear at least twice
	if strings.Count(string(src), "missingEnvelopePieces(cfg") < 2 {
		t.Fatal("Dispatch and DispatchStream must both consult missingEnvelopePieces")
	}
}
