package billing

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestModelPrice(t *testing.T) {
	if p := ModelPrice("claude-opus-4-8"); p.InputPer1M != 5 || p.OutputPer1M != 25 {
		t.Fatalf("opus=%+v", p)
	}
	if p := ModelPrice("claude-haiku-4-5"); p.InputPer1M != 1 || p.OutputPer1M != 5 {
		t.Fatalf("haiku=%+v", p)
	}
	if p := ModelPrice("claude-sonnet-4-6"); p.InputPer1M != 3 || p.OutputPer1M != 15 {
		t.Fatalf("sonnet=%+v", p)
	}
	if p := ModelPrice("unknown-model"); p.InputPer1M != 3 || p.OutputPer1M != 15 {
		t.Fatalf("default=%+v", p)
	}
}

func TestCostUsd(t *testing.T) {
	// opus: 1M input + 1M output = 5 + 25 = 30
	if c := CostUsd("opus", 1_000_000, 1_000_000, 0, 0); !approx(c, 30) {
		t.Fatalf("cost=%v, want 30", c)
	}
	// cache read 1M on opus = 1M * 5 * 0.1 / 1M = 0.5
	if c := CostUsd("opus", 0, 0, 1_000_000, 0); !approx(c, 0.5) {
		t.Fatalf("cacheRead cost=%v, want 0.5", c)
	}
	// cache create 1M on opus = 1M * 5 * 1.25 / 1M = 6.25
	if c := CostUsd("opus", 0, 0, 0, 1_000_000); !approx(c, 6.25) {
		t.Fatalf("cacheCreate cost=%v, want 6.25", c)
	}
	if c := CostUsd("opus", 0, 0, 0, 0); !approx(c, 0) {
		t.Fatalf("zero=%v", c)
	}
}

func TestKnownModel(t *testing.T) {
	if !KnownModel("claude-opus-4-8") {
		t.Fatal("claude-opus-4-8 should be known")
	}
	if !KnownModel("claude-sonnet-4-6") {
		t.Fatal("claude-sonnet-4-6 should be known")
	}
	if !KnownModel("claude-haiku-4-5") {
		t.Fatal("claude-haiku-4-5 should be known")
	}
	if KnownModel("gpt-4") {
		t.Fatal("gpt-4 should not be known")
	}
	if KnownModel("unknown-model") {
		t.Fatal("unknown-model should not be known")
	}
}

func TestCostUsdFull_ReferenceOpus(t *testing.T) {
	// Reference: claude-opus-4-8, inTok=2, outTok=7353, cacheRead=301197, cache5m=0, cache1h=5154
	// Expected ≈ 0.385974 (within 1e-5)
	// Breakdown (opus InputPer1M=5, OutputPer1M=25):
	//   input:      2       * 5  / 1e6 = 0.00001
	//   output:     7353    * 25 / 1e6 = 0.183825
	//   cacheRead:  301197  * 5 * 0.1 / 1e6 = 0.150598...5
	//   cache5m:    0       * 5 * 1.25 / 1e6 = 0
	//   cache1h:    5154    * 5 * 2.0  / 1e6 = 0.05154
	//   total ≈ 0.385973...
	got := CostUsdFull("claude-opus-4-8", 2, 7353, 301197, 0, 5154)
	want := 0.385974 // spec reference value
	if math.Abs(got-want) > 1e-5 {
		t.Fatalf("CostUsdFull reference example: got %v, want ~%v (diff=%v)", got, want, math.Abs(got-want))
	}
}
