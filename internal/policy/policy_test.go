package policy

import (
	"reflect"
	"testing"
)

func ptrI(i int) *int             { return &i }
func ptrB(b bool) *bool           { return &b }
func ptrF(f float64) *float64     { return &f }
func ptrSS(ss []string) *[]string { return &ss }
func ptrRF(rf RangeF) *RangeF     { return &rf }
func ptrI64(i int64) *int64       { return &i }

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
		Patch{MaxConcurrent: ptrI(5)},                              // group layer
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

func TestSpendCapDefaults(t *testing.T) {
	d := Defaults()
	if d.SpendCap5hUsd.Min != 100 || d.SpendCap5hUsd.Max != 200 {
		t.Fatal("5h 默认区间错")
	}
}

func TestSpendCapPatch(t *testing.T) {
	c := Defaults()
	en := true
	apply(&c, Patch{SpendCap5hEnabled: &en, SpendCap5hUsd: ptrRF(RangeF{Min: 50, Max: 60})})
	if !c.SpendCap5hEnabled || c.SpendCap5hUsd.Max != 60 {
		t.Fatal("patch 未生效")
	}
}

func TestCCEnvelopeDefaultsAndApply(t *testing.T) {
	d := Defaults()
	if d.CCEnvelopeEnabled || d.CCEnforceSystemPrompt || d.CCEnforceBetaParam || d.CCEnforceCliHeaders {
		t.Fatal("CC envelope toggles must default false")
	}
	if d.CCSystemPromptText == "" || d.CCCliXApp == "" {
		t.Fatal("CC value defaults must be set")
	}
	on := true
	act := "complete"
	c := Defaults()
	apply(&c, Patch{CCEnvelopeEnabled: &on, CCEnforceBetaParam: &on, CCEnvelopeAction: &act})
	if !c.CCEnvelopeEnabled || !c.CCEnforceBetaParam || c.CCEnvelopeAction != "complete" {
		t.Fatalf("apply did not patch CC fields: %+v", c)
	}
}

func TestQuietWindowOvernight(t *testing.T) {
	// 21:00-04:00 跨夜: 23:00(=1380) 命中, 12:00(=720) 不命中
	windows := []TimeWindow{{StartMin: 1260, EndMin: 240}}
	if !InAnyWindow(23*60, windows) {
		t.Fatal("23点应命中安静窗口")
	}
	if InAnyWindow(12*60, windows) {
		t.Fatal("12点不应命中安静窗口")
	}
	// Normal window (non-overnight): 09:00-18:00
	normal := []TimeWindow{{StartMin: 9 * 60, EndMin: 18 * 60}}
	if !InAnyWindow(10*60, normal) {
		t.Fatal("10点应命中正常窗口")
	}
	if InAnyWindow(20*60, normal) {
		t.Fatal("20点不应命中正常窗口")
	}
	// Edge: exactly at start
	if !InAnyWindow(1260, windows) {
		t.Fatal("21:00整应命中安静窗口")
	}
	// Edge: exactly at end (exclusive)
	if InAnyWindow(240, windows) {
		t.Fatal("04:00整不应命中(端点排除)")
	}
}
