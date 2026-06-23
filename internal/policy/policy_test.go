package policy

import (
	"reflect"
	"testing"
)

func ptrI(i int) *int         { return &i }
func ptrB(b bool) *bool       { return &b }
func ptrF(f float64) *float64 { return &f }
func ptrSS(ss []string) *[]string { return &ss }

func TestMaxTokensFor(t *testing.T) {
	c := Defaults()
	cases := []struct {
		model string
		want  int
	}{
		{"claude-opus-4-8", 128000},
		{"claude-haiku-4-5-20251001", 64000}, // dated suffix matches the base key
		{"claude-sonnet-4-6", 64000},
		{"claude-3-opus-20240229", 0}, // no key matches → unlimited
		{"gpt-4", 0},
	}
	for _, tc := range cases {
		if got := c.MaxTokensFor(tc.model); got != tc.want {
			t.Errorf("MaxTokensFor(%q)=%d, want %d", tc.model, got, tc.want)
		}
	}
	// Tenant patch overrides the whole map.
	patched := Resolve(c, Patch{ModelMaxTokens: &map[string]int{"claude-opus-4-8": 32000}})
	if got := patched.MaxTokensFor("claude-opus-4-8"); got != 32000 {
		t.Errorf("patched opus limit=%d, want 32000", got)
	}
}

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

func TestResolve_TenantOverGlobal(t *testing.T) {
	base := Defaults()
	global := Patch{MaxConcurrent: ptrI(5), FallbackEnabled: ptrB(true)}
	tenant := Patch{MaxConcurrent: ptrI(2)} // tenant layer wins for MaxConcurrent
	got := Resolve(base, global, tenant)
	if got.MaxConcurrent != 2 {
		t.Fatalf("MaxConcurrent=%d, want 2 (tenant patch wins over global)", got.MaxConcurrent)
	}
	// Global-only field is preserved when tenant does not override it.
	if !got.FallbackEnabled {
		t.Fatal("FallbackEnabled should remain true from global layer")
	}
	// Unset-in-both fields keep the base value.
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

func TestResolve_QuotaRotateThreshold(t *testing.T) {
	base := Defaults() // default 0.95

	// Valid value applies.
	got := Resolve(base, Patch{QuotaRotateThreshold: ptrF(0.8)})
	if got.QuotaRotateThreshold != 0.8 {
		t.Fatalf("QuotaRotateThreshold=%v, want 0.8", got.QuotaRotateThreshold)
	}

	// Invalid value (>1) falls back to 0.95.
	got = Resolve(base, Patch{QuotaRotateThreshold: ptrF(1.5)})
	if got.QuotaRotateThreshold != 0.95 {
		t.Fatalf("QuotaRotateThreshold=%v, want 0.95 (out-of-range reset)", got.QuotaRotateThreshold)
	}

	// Invalid value (<=0) falls back to 0.95.
	got = Resolve(base, Patch{QuotaRotateThreshold: ptrF(0)})
	if got.QuotaRotateThreshold != 0.95 {
		t.Fatalf("QuotaRotateThreshold=%v, want 0.95 (zero reset)", got.QuotaRotateThreshold)
	}

	// DryRun reports the changed field.
	_, diffs := DryRun(base, Patch{QuotaRotateThreshold: ptrF(0.7)})
	seen := map[string]Diff{}
	for _, d := range diffs {
		seen[d.Field] = d
	}
	if _, ok := seen["QuotaRotateThreshold"]; !ok {
		t.Fatal("DryRun missing diff for QuotaRotateThreshold")
	}
	if d := seen["QuotaRotateThreshold"]; d.To != "0.7" {
		t.Fatalf("QuotaRotateThreshold diff To=%q, want 0.7", d.To)
	}
}

func TestResolve_MaxFailover(t *testing.T) {
	base := Defaults() // default 50

	// Custom value applies.
	got := Resolve(base, Patch{MaxFailover: ptrI(10)})
	if got.MaxFailover != 10 {
		t.Fatalf("MaxFailover=%d, want 10", got.MaxFailover)
	}

	// DryRun reports the changed field.
	_, diffs := DryRun(base, Patch{MaxFailover: ptrI(20)})
	seen := map[string]Diff{}
	for _, d := range diffs {
		seen[d.Field] = d
	}
	if _, ok := seen["MaxFailover"]; !ok {
		t.Fatal("DryRun missing diff for MaxFailover")
	}
	if d := seen["MaxFailover"]; d.To != "20" {
		t.Fatalf("MaxFailover diff To=%q, want 20", d.To)
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

// TestPickThreshold covers the shared global-policy pickup used by both the
// meridian poller and the CPA discovery loop so they gate on the same value.
func TestPickThreshold(t *testing.T) {
	const def = 0.95
	cases := []struct {
		name string
		json []byte
		want float64
	}{
		{"valid 0.8", []byte(`{"QuotaRotateThreshold":0.8}`), 0.8},
		{"valid 1.0", []byte(`{"QuotaRotateThreshold":1.0}`), 1.0},
		{"field absent", []byte(`{}`), def},
		{"out of range high", []byte(`{"QuotaRotateThreshold":1.5}`), def},
		{"out of range zero", []byte(`{"QuotaRotateThreshold":0}`), def},
		{"empty json", []byte(``), def},
		{"invalid json", []byte(`not-json`), def},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PickThreshold(tc.json, def); got != tc.want {
				t.Fatalf("PickThreshold(%q, %v) = %v, want %v", tc.json, def, got, tc.want)
			}
		})
	}
}

// TestPickMaxConcurrent covers the shared MaxConcurrent pickup so CPA and
// meridian per-account capacity track the live global policy identically.
func TestPickMaxConcurrent(t *testing.T) {
	const def = 3
	cases := []struct {
		name string
		json []byte
		want int
	}{
		{"override 7", []byte(`{"MaxConcurrent":7}`), 7},
		{"field absent", []byte(`{}`), def},
		{"non-positive ignored", []byte(`{"MaxConcurrent":0}`), def},
		{"negative ignored", []byte(`{"MaxConcurrent":-2}`), def},
		{"empty json", []byte(``), def},
		{"invalid json", []byte(`not-json`), def},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PickMaxConcurrent(tc.json, def); got != tc.want {
				t.Fatalf("PickMaxConcurrent(%q, %v) = %v, want %v", tc.json, def, got, tc.want)
			}
		})
	}
}
