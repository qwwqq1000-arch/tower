package policy

import (
	"reflect"
	"testing"
)

func ptrI(i int) *int         { return &i }
func ptrB(b bool) *bool       { return &b }
func ptrF(f float64) *float64 { return &f }
func ptrSS(ss []string) *[]string { return &ss }

func TestResolve_LayeredOverride(t *testing.T) {
	base := Defaults()
	got := Resolve(base,
		Patch{MaxConcurrent: ptrI(5)},          // group layer
		Patch{MaxConcurrent: ptrI(2), FallbackEnabled: ptrB(true)}, // node layer wins for MaxConcurrent
	)
	if got.MaxConcurrent != 2 {
		t.Fatalf("MaxConcurrent=%d, want 2 (last patch wins)", got.MaxConcurrent)
	}
	if !got.FallbackEnabled {
		t.Fatal("FallbackEnabled should be true")
	}
	if got.BanPersistStreak != base.BanPersistStreak {
		t.Fatal("unset fields keep base value")
	}
}

func TestResolve_NilPatchNoChange(t *testing.T) {
	base := Defaults()
	got := Resolve(base, Patch{})
	if !reflect.DeepEqual(got, base) {
		t.Fatalf("empty patch changed config: %+v vs %+v", got, base)
	}
}

func TestDryRun_ReportsDiffs(t *testing.T) {
	base := Defaults() // MaxConcurrent default e.g. 3
	final, diffs := DryRun(base, Patch{MaxConcurrent: ptrI(10), FallbackEnabled: ptrB(true)})
	if final.MaxConcurrent != 10 {
		t.Fatalf("final MaxConcurrent=%d", final.MaxConcurrent)
	}
	// expect at least the two changed fields reported
	seen := map[string]Diff{}
	for _, d := range diffs {
		seen[d.Field] = d
	}
	if _, ok := seen["MaxConcurrent"]; !ok {
		t.Fatal("MaxConcurrent diff missing")
	}
	if d := seen["MaxConcurrent"]; d.To != "10" {
		t.Fatalf("MaxConcurrent To=%q, want 10", d.To)
	}
	if _, ok := seen["FallbackEnabled"]; !ok {
		t.Fatal("FallbackEnabled diff missing")
	}
}

func TestDryRun_NoChangeNoDiffs(t *testing.T) {
	base := Defaults()
	_, diffs := DryRun(base, Patch{MaxConcurrent: ptrI(base.MaxConcurrent)})
	if len(diffs) != 0 {
		t.Fatalf("setting same value should produce 0 diffs, got %v", diffs)
	}
}

func TestResolve_FallbackStrategyTriggers(t *testing.T) {
	base := Defaults()
	patch := Patch{
		FallbackKeywords:     ptrSS([]string{"danger"}),
		FallbackModels:       ptrSS([]string{"claude-opus-4-7"}),
		FallbackProbeEnabled: ptrB(true),
	}

	// Resolve applies all three fields.
	got := Resolve(base, patch)
	if !reflect.DeepEqual(got.FallbackKeywords, []string{"danger"}) {
		t.Fatalf("FallbackKeywords=%v, want [danger]", got.FallbackKeywords)
	}
	if !reflect.DeepEqual(got.FallbackModels, []string{"claude-opus-4-7"}) {
		t.Fatalf("FallbackModels=%v, want [claude-opus-4-7]", got.FallbackModels)
	}
	if !got.FallbackProbeEnabled {
		t.Fatal("FallbackProbeEnabled should be true")
	}

	// DryRun reports the three changed fields.
	_, diffs := DryRun(base, patch)
	seen := map[string]Diff{}
	for _, d := range diffs {
		seen[d.Field] = d
	}
	for _, field := range []string{"FallbackKeywords", "FallbackModels", "FallbackProbeEnabled"} {
		if _, ok := seen[field]; !ok {
			t.Fatalf("DryRun missing diff for %s", field)
		}
	}
}
