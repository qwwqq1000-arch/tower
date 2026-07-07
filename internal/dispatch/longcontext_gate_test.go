package dispatch

import (
	"os"
	"strings"
	"testing"
)

func TestBothTiersComputeLongCtx(t *testing.T) {
	src, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatal(err)
	}
	n := strings.Count(string(src), "isLongContextRequest(model, body, cfg)")
	if n < 2 {
		t.Fatalf("expected isLongContextRequest computed in both Dispatch and DispatchStream, found %d", n)
	}
}
