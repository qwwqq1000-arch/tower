package dispatch

import (
	"os"
	"strings"
	"testing"
)

func TestBothTiersMarkNo1M(t *testing.T) {
	src, _ := os.ReadFile("service.go")
	s := string(src)
	if strings.Count(s, "isExtraUsageNo1M(") < 2 {
		t.Fatalf("isExtraUsageNo1M must be checked in both tiers")
	}
	if strings.Count(s, "s.markNo1M(") < 2 {
		t.Fatalf("markNo1M must be called in both tiers")
	}
}
