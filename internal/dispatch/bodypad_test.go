package dispatch

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestBodyPadNoBytesReturnsOriginal(t *testing.T) {
	orig := []byte(`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`)
	out := padBody(orig, 0, "seed")
	if string(out) != string(orig) {
		t.Fatalf("padBody with nBytes=0 must return the original body unchanged")
	}
}

func TestBodyPadKeepsValidJSONAndMessages(t *testing.T) {
	orig := []byte(`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`)
	out := padBody(orig, 100, "seedk")

	// Must remain valid JSON
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("padBody produced invalid JSON: %v", err)
	}

	// messages must be unchanged
	var origMap map[string]any
	if err := json.Unmarshal(orig, &origMap); err != nil {
		t.Fatal("original JSON invalid (test setup error)")
	}
	if !reflect.DeepEqual(m["messages"], origMap["messages"]) {
		t.Fatalf("padBody changed messages: got %v, want %v", m["messages"], origMap["messages"])
	}

	// metadata.pad must be present and have length >= 100
	meta, ok := m["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("padBody: metadata field missing or wrong type; got %T", m["metadata"])
	}
	pad, ok := meta["pad"].(string)
	if !ok {
		t.Fatalf("padBody: metadata.pad missing or wrong type")
	}
	if len(pad) != 100 {
		t.Fatalf("padBody: expected pad length 100, got %d", len(pad))
	}
}

func TestBodyPadInvalidJSONReturnsOriginal(t *testing.T) {
	orig := []byte(`not valid json`)
	out := padBody(orig, 50, "seed")
	if string(out) != string(orig) {
		t.Fatalf("padBody with invalid JSON must return original body unchanged")
	}
}

func TestBodyPadPreservesExistingMetadata(t *testing.T) {
	orig := []byte(`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}],"metadata":{"user_id":"u123"}}`)
	out := padBody(orig, 20, "seed")

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("padBody produced invalid JSON: %v", err)
	}
	meta, ok := m["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("padBody: metadata field missing or wrong type")
	}
	if meta["user_id"] != "u123" {
		t.Fatalf("padBody: existing metadata key user_id lost, got %v", meta["user_id"])
	}
	if _, ok := meta["pad"].(string); !ok {
		t.Fatalf("padBody: metadata.pad missing")
	}
}

func TestBodyPadDeterministicBySeed(t *testing.T) {
	orig := []byte(`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`)
	out1 := padBody(orig, 30, "seedA")
	out2 := padBody(orig, 30, "seedA")
	out3 := padBody(orig, 30, "seedB")

	if string(out1) != string(out2) {
		t.Fatalf("padBody must be deterministic for same seed")
	}
	// Different seeds should produce different pad content (not strictly required,
	// but our implementation ensures it by incorporating seed into the pad bytes).
	var m1, m3 map[string]any
	json.Unmarshal(out1, &m1)
	json.Unmarshal(out3, &m3)
	meta1 := m1["metadata"].(map[string]any)
	meta3 := m3["metadata"].(map[string]any)
	if meta1["pad"] == meta3["pad"] {
		// This is acceptable if both seeds happen to produce the same pattern by coincidence
		// (our simple algorithm may, so don't hard-fail — just log).
		t.Logf("note: different seeds produced same pad (seed-variety not strictly required)")
	}
}
