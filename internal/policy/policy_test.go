package policy

import "testing"

func ptrI(i int) *int       { return &i }
func ptrB(b bool) *bool     { return &b }
func ptrF(f float64) *float64 { return &f }

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
	if got != base {
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
