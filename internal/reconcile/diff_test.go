package reconcile

import "testing"

func TestDiff(t *testing.T) {
	desired := map[string]map[string]any{
		"opencode": {"memory": true, "thinking": "adaptive"},
		"crush":    {"memory": true},
	}
	actual := map[string]map[string]any{
		"opencode": {"memory": false, "thinking": "adaptive"}, // memory differs
		// crush missing entirely
	}
	d := Diff(desired, actual)
	if len(d["opencode"]) != 1 || d["opencode"]["memory"] != true {
		t.Fatalf("opencode diff=%v, want {memory:true}", d["opencode"])
	}
	if d["crush"]["memory"] != true {
		t.Fatalf("crush should be fully patched: %v", d["crush"])
	}
}

func TestDiff_NoChange(t *testing.T) {
	m := map[string]map[string]any{"opencode": {"memory": true}}
	if len(Diff(m, m)) != 0 {
		t.Fatal("identical → no diff")
	}
}
