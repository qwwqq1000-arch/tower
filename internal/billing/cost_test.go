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
