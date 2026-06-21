package session

import (
	"encoding/json"
	"testing"
)

// ---- ConvID tests ----

func TestConvID_Stable(t *testing.T) {
	body := []byte(`{"system":"be helpful","messages":[{"role":"user","content":"hello"}]}`)
	id1 := ConvID(body)
	id2 := ConvID(body)
	if id1 == "" {
		t.Fatal("ConvID returned empty string for valid body")
	}
	if id1 != id2 {
		t.Fatalf("ConvID not stable: %q vs %q", id1, id2)
	}
	if len(id1) != 16 {
		t.Fatalf("ConvID length=%d, want 16", len(id1))
	}
}

func TestConvID_DifferentMessages(t *testing.T) {
	body1 := []byte(`{"system":"be helpful","messages":[{"role":"user","content":"hello"}]}`)
	body2 := []byte(`{"system":"be helpful","messages":[{"role":"user","content":"goodbye"}]}`)
	if ConvID(body1) == ConvID(body2) {
		t.Fatal("different user messages should produce different ConvIDs")
	}
}

func TestConvID_DifferentSystems(t *testing.T) {
	body1 := []byte(`{"system":"sys-a","messages":[{"role":"user","content":"same"}]}`)
	body2 := []byte(`{"system":"sys-b","messages":[{"role":"user","content":"same"}]}`)
	if ConvID(body1) == ConvID(body2) {
		t.Fatal("different systems should produce different ConvIDs")
	}
}

func TestConvID_ArrayContent(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	id := ConvID(body)
	if id == "" {
		t.Fatal("ConvID should work with array content")
	}
	// same body → same id
	if ConvID(body) != id {
		t.Fatal("ConvID not stable with array content")
	}
}

func TestConvID_Unparseable(t *testing.T) {
	if ConvID([]byte("not json")) != "" {
		t.Fatal("unparseable body should return empty string")
	}
}

func TestConvID_NoSignal(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","content":"hi"}]}`)
	if ConvID(body) != "" {
		t.Fatal("body with no system and no user message should return empty string")
	}
}

func TestConvID_SkipsAssistantMessages(t *testing.T) {
	// Only assistant messages — no user message → no signal
	body := []byte(`{"messages":[{"role":"assistant","content":"answer"}]}`)
	if ConvID(body) != "" {
		t.Fatal("no user message should give empty id")
	}
}

func TestConvID_MultipleMessages_UsesFirst(t *testing.T) {
	// Ensure we use the first user message, not a later one
	body1 := []byte(`{"messages":[{"role":"user","content":"first"},{"role":"user","content":"second"}]}`)
	body2 := []byte(`{"messages":[{"role":"user","content":"first"},{"role":"user","content":"other"}]}`)
	if ConvID(body1) != ConvID(body2) {
		t.Fatal("ConvID should only use the first user message")
	}
}

// helper: build body as JSON bytes
func mkBody(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

// ---- Store tests ----

func TestStore_RecordError_ReachesExile(t *testing.T) {
	s := NewStore()
	const (
		conv      = "conv1"
		threshold = int64(3)
		cooldown  = int64(60_000) // 1 min
		now       = int64(1_000_000)
	)
	for i := 0; i < 2; i++ {
		s.RecordError(conv, threshold, cooldown, now)
		if s.Exiled(conv, now) {
			t.Fatalf("should not be exiled after %d errors (threshold=%d)", i+1, threshold)
		}
	}
	s.RecordError(conv, threshold, cooldown, now)
	if !s.Exiled(conv, now) {
		t.Fatal("should be exiled after reaching threshold")
	}
}

func TestStore_Exiled_TrueWithinWindow_FalseAfter(t *testing.T) {
	s := NewStore()
	const cooldown = int64(5_000)
	const now = int64(10_000)
	s.ForceExile("conv2", cooldown, now)
	if !s.Exiled("conv2", now) {
		t.Fatal("should be exiled immediately after ForceExile")
	}
	if !s.Exiled("conv2", now+cooldown-1) {
		t.Fatal("should still be exiled just before window ends")
	}
	if s.Exiled("conv2", now+cooldown) {
		t.Fatal("should not be exiled at exactly cooldown boundary")
	}
	if s.Exiled("conv2", now+cooldown+1) {
		t.Fatal("should not be exiled after window expires")
	}
}

func TestStore_RecordSuccess_ClearsExile(t *testing.T) {
	s := NewStore()
	const now = int64(0)
	s.ForceExile("conv3", 60_000, now)
	if !s.Exiled("conv3", now) {
		t.Fatal("should be exiled before success")
	}
	s.RecordSuccess("conv3")
	if s.Exiled("conv3", now) {
		t.Fatal("RecordSuccess should clear exile")
	}
}

func TestStore_RecordSuccess_ClearsErrorCount(t *testing.T) {
	s := NewStore()
	const (
		conv      = "conv4"
		threshold = int64(3)
		cooldown  = int64(60_000)
		now       = int64(0)
	)
	s.RecordError(conv, threshold, cooldown, now)
	s.RecordError(conv, threshold, cooldown, now)
	s.RecordSuccess(conv)
	// After success, need threshold more errors to exile
	s.RecordError(conv, threshold, cooldown, now)
	s.RecordError(conv, threshold, cooldown, now)
	if s.Exiled(conv, now) {
		t.Fatal("after RecordSuccess the error count should have reset")
	}
	s.RecordError(conv, threshold, cooldown, now)
	if !s.Exiled(conv, now) {
		t.Fatal("should be exiled after threshold errors post-success")
	}
}

func TestStore_ForceExile_Sets(t *testing.T) {
	s := NewStore()
	const now = int64(5_000)
	const cooldown = int64(30_000)
	s.ForceExile("conv5", cooldown, now)
	if !s.Exiled("conv5", now) {
		t.Fatal("ForceExile should immediately exile the conversation")
	}
	if s.Exiled("conv5", now+cooldown+1) {
		t.Fatal("exile should expire after cooldown")
	}
}

func TestStore_Exiled_EmptyConv(t *testing.T) {
	s := NewStore()
	if s.Exiled("", 0) {
		t.Fatal("empty conv should never be exiled")
	}
}

func TestStore_RecordError_ThresholdZeroDisabled(t *testing.T) {
	s := NewStore()
	const now = int64(0)
	for i := 0; i < 100; i++ {
		s.RecordError("conv6", 0, 60_000, now)
	}
	if s.Exiled("conv6", now) {
		t.Fatal("threshold=0 means disabled, should never exile")
	}
}

func TestStore_ErrorCountResetAfterExile(t *testing.T) {
	s := NewStore()
	const (
		conv      = "conv7"
		threshold = int64(2)
		cooldown  = int64(60_000)
		now       = int64(0)
	)
	// Hit threshold → exile + count resets to 0
	s.RecordError(conv, threshold, cooldown, now)
	s.RecordError(conv, threshold, cooldown, now)
	if !s.Exiled(conv, now) {
		t.Fatal("should be exiled")
	}
	// After exile triggered, errs resets to 0
	// One more error should not immediately re-exile (need threshold again)
	s.RecordError(conv, threshold, cooldown, now+1)
	// Still exiled from previous window
	if !s.Exiled(conv, now+1) {
		t.Fatal("should still be exiled (window not expired)")
	}
}
