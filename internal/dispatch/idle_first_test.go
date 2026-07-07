package dispatch

import (
	"testing"
)

func TestOrderIdleFirst_BasicOrder(t *testing.T) {
	// [a, b, c] with inflightOf{a:3, b:0, c:1} → result must be [b, c, a]
	keys := []string{"a", "b", "c"}
	inflightOf := map[string]int{"a": 3, "b": 0, "c": 1}
	orderIdleFirst(keys, inflightOf)
	if keys[0] != "b" || keys[1] != "c" || keys[2] != "a" {
		t.Errorf("expected [b c a], got %v", keys)
	}
}

func TestOrderIdleFirst_RandomTiebreak(t *testing.T) {
	// All inflight == 0 → purely random order. Over 200 runs, each key must appear first at
	// least once (otherwise the tiebreak is not working and concentration remains).
	firstCounts := map[string]int{}
	for i := 0; i < 200; i++ {
		keys := []string{"a", "b", "c"}
		orderIdleFirst(keys, map[string]int{"a": 0, "b": 0, "c": 0})
		firstCounts[keys[0]]++
	}
	for _, k := range []string{"a", "b", "c"} {
		if firstCounts[k] == 0 {
			t.Errorf("key %q never appeared first over 200 runs: counts %v (tiebreak not working)", k, firstCounts)
		}
	}
}

func TestOrderIdleFirst_Empty(t *testing.T) {
	// Must not panic on empty slice.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic on empty slice: %v", r)
		}
	}()
	orderIdleFirst([]string{}, map[string]int{})
}

func TestOrderIdleFirst_SingleElement(t *testing.T) {
	keys := []string{"a"}
	orderIdleFirst(keys, map[string]int{"a": 5})
	if keys[0] != "a" {
		t.Errorf("expected [a], got %v", keys)
	}
}

func TestOrderIdleFirst_MissingFromMap(t *testing.T) {
	// Keys absent from inflightOf default to 0 inflight (treated as idle).
	keys := []string{"a", "b", "c"}
	inflightOf := map[string]int{"a": 5} // b and c absent → 0
	orderIdleFirst(keys, inflightOf)
	// a must be last (most busy); b and c (both 0) come first in some order
	if keys[2] != "a" {
		t.Errorf("expected 'a' last (most busy), got %v", keys)
	}
}
