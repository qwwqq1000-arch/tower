package dispatch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

// fillChannelSlots occupies every slot of a fallback channel's slot set so that
// the next TryDispatch on it must fail (slot set full).
func fillChannelSlots(store *state.Store, channelID string, cap int) {
	key := fbSlotKey(channelID)
	store.Ensure(key, cap)
	bk := state.BreakerCfg{PersistStreak: 1 << 30, BaseMs: 0, MaxMs: 0, Mult: 1}
	for i := 0; i < cap; i++ {
		if !store.TryDispatch(key, "opus", bk) {
			panic("could not pre-fill slot")
		}
	}
}

// TestViaChannel_RejectsWhenSlotsFull asserts that when a fallback channel's
// MaxConcurrent slot set is full, viaChannel does NOT forward the request to the
// upstream channel and instead returns backpressure (503). Regression for
// fallback-2: MaxConcurrent must actually cap concurrency. Runs without a DB: the
// reject path returns the 503 Outcome before any persistence (Q stays nil).
func TestViaChannel_RejectsWhenSlotsFull(t *testing.T) {
	var hits int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()

	store := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	svc := &Service{Store: store, Base: policy.Defaults(), Now: func() int64 { return 0 }}

	ch := sqlc.FallbackChannel{ID: "ch_full", Name: "full", BaseUrl: up.URL, ApiKey: "k", MaxConcurrent: 1}
	fillChannelSlots(store, ch.ID, int(ch.MaxConcurrent))

	out, served := svc.viaChannel(context.Background(), "owner1", "opus", []byte(`{"model":"opus"}`), ch, "exhausted", 0, policy.Defaults())

	if got := atomic.LoadInt64(&hits); got != 0 {
		t.Fatalf("upstream was forwarded %d times despite full slot set; MaxConcurrent not enforced", got)
	}
	if served {
		t.Fatal("viaChannel reported served=true on a full channel; caller would not fail over")
	}
	if out.Status != 503 {
		t.Fatalf("expected 503 backpressure when slots full, got status=%d body=%s", out.Status, out.Body)
	}
}

// TestViaChannels_AllFull_Returns503 asserts that viaChannels iterates EVERY
// fallback channel and only returns the terminal 503 when all of them are at
// capacity — never forwarding to a full channel (fallback-1: failover across
// channels instead of 503-ing on channels[0]). Q-free: every channel hits the
// reject path before any persistence.
func TestViaChannels_AllFull_Returns503(t *testing.T) {
	var hits int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()

	store := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	svc := &Service{Store: store, Base: policy.Defaults(), Now: func() int64 { return 0 }}

	ch1 := sqlc.FallbackChannel{ID: "ch_a", Name: "a", BaseUrl: up.URL, ApiKey: "k", MaxConcurrent: 1, Priority: 10}
	ch2 := sqlc.FallbackChannel{ID: "ch_b", Name: "b", BaseUrl: up.URL, ApiKey: "k", MaxConcurrent: 1, Priority: 5}
	fillChannelSlots(store, ch1.ID, 1)
	fillChannelSlots(store, ch2.ID, 1)

	out := svc.viaChannels(context.Background(), "owner1", "opus", []byte(`{"model":"opus"}`), []sqlc.FallbackChannel{ch1, ch2}, "exhausted", 0, policy.Defaults())

	if got := atomic.LoadInt64(&hits); got != 0 {
		t.Fatalf("forwarded %d times though all channels were full", got)
	}
	if out.Status != 503 {
		t.Fatalf("expected 503 after all channels full, got status=%d body=%s", out.Status, out.Body)
	}
}

// TestStreamChannels_AllFull_NotCommitted is the streaming analogue: when every
// channel is full, streamChannels writes nothing to the client and reports
// committed=false (the caller then falls through to its own 503).
func TestStreamChannels_AllFull_NotCommitted(t *testing.T) {
	var hits int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {}\n\n"))
	}))
	defer up.Close()

	store := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	svc := &Service{Store: store, Base: policy.Defaults(), Now: func() int64 { return 0 }}

	ch1 := sqlc.FallbackChannel{ID: "ch_sa", Name: "a", BaseUrl: up.URL, ApiKey: "k", MaxConcurrent: 1}
	ch2 := sqlc.FallbackChannel{ID: "ch_sb", Name: "b", BaseUrl: up.URL, ApiKey: "k", MaxConcurrent: 1}
	fillChannelSlots(store, ch1.ID, 1)
	fillChannelSlots(store, ch2.ID, 1)

	w := httptest.NewRecorder()
	_, committed := svc.streamChannels(context.Background(), w, []sqlc.FallbackChannel{ch1, ch2}, []byte(`{"model":"opus"}`), "owner1", "opus", "exhausted", policy.Defaults())

	if got := atomic.LoadInt64(&hits); got != 0 {
		t.Fatalf("stream forwarded %d times though all channels were full", got)
	}
	if committed {
		t.Fatal("streamChannels committed though all channels were full")
	}
}

// TestStreamChannel_RejectsWhenSlotsFull asserts the streaming path likewise
// declines to forward (committed=false) when the slot set is full. Runs without
// a DB: the reject path returns before any persistence.
func TestStreamChannel_RejectsWhenSlotsFull(t *testing.T) {
	var hits int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {}\n\n"))
	}))
	defer up.Close()

	store := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	svc := &Service{Store: store, Base: policy.Defaults(), Now: func() int64 { return 0 }}

	ch := sqlc.FallbackChannel{ID: "ch_full_s", Name: "full", BaseUrl: up.URL, ApiKey: "k", MaxConcurrent: 1}
	fillChannelSlots(store, ch.ID, int(ch.MaxConcurrent))

	w := httptest.NewRecorder()
	_, committed := svc.streamChannel(context.Background(), w, ch, []byte(`{"model":"opus"}`), "owner1", "opus", "exhausted", policy.Defaults())

	if got := atomic.LoadInt64(&hits); got != 0 {
		t.Fatalf("upstream stream forwarded %d times despite full slot set", got)
	}
	if committed {
		t.Fatal("streamChannel committed a response despite full slot set; MaxConcurrent not enforced")
	}
}
