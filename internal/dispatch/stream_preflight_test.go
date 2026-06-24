package dispatch

import (
	"io"
	"strings"
	"testing"
)

// ── sseHasContentMarker ──────────────────────────────────────────────────────

func TestSseHasContentMarker_TrueOnDelta(t *testing.T) {
	data := []byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n")
	if !sseHasContentMarker(data) {
		t.Fatal("expected sseHasContentMarker=true for buffer containing content_block_delta")
	}
}

func TestSseHasContentMarker_FalseOnlyMessageStart(t *testing.T) {
	// message_start + content_block_start but no delta: upstream that dies right after
	data := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-4-5\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":15,\"output_tokens\":1}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: ping\ndata: {\"type\":\"ping\"}\n\n")
	if sseHasContentMarker(data) {
		t.Fatal("expected sseHasContentMarker=false for buffer with only message_start + content_block_start (no delta)")
	}
}

func TestSseHasContentMarker_FalseOnEmpty(t *testing.T) {
	if sseHasContentMarker([]byte{}) {
		t.Fatal("expected sseHasContentMarker=false for empty buffer")
	}
}

func TestSseHasContentMarker_FalseOnPingOnly(t *testing.T) {
	data := []byte("event: ping\ndata: {\"type\":\"ping\"}\n\n")
	if sseHasContentMarker(data) {
		t.Fatal("expected sseHasContentMarker=false for ping-only SSE")
	}
}

// ── readUntilContent ─────────────────────────────────────────────────────────

func TestReadUntilContent_FullStream_HasContent(t *testing.T) {
	// Normal stream: message_start → content_block_start → content_block_delta
	sse := "event: message_start\ndata: {}\n\n" +
		"event: content_block_start\ndata: {}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\"}\n\n" +
		"event: message_delta\ndata: {}\n\nevent: message_stop\ndata: {}\n\n"

	prefix, hasContent := readUntilContent(strings.NewReader(sse), firstContentCapBytes)
	if !hasContent {
		t.Fatal("expected hasContent=true for a normal full stream")
	}
	if !strings.Contains(string(prefix), "message_start") {
		t.Error("prefix should contain message_start preamble")
	}
	if !strings.Contains(string(prefix), "content_block_start") {
		t.Error("prefix should contain content_block_start")
	}
	if !strings.Contains(string(prefix), "content_block_delta") {
		t.Error("prefix should contain content_block_delta")
	}
}

func TestReadUntilContent_EmptyStreamBeforeDelta_NoContent(t *testing.T) {
	// The bug case: message_start + content_block_start then EOF (no delta)
	sse := "event: message_start\ndata: {}\n\n" +
		"event: content_block_start\ndata: {}\n\n"

	prefix, hasContent := readUntilContent(strings.NewReader(sse), firstContentCapBytes)
	if hasContent {
		t.Fatal("expected hasContent=false when stream ends before any content_block_delta")
	}
	if !strings.Contains(string(prefix), "message_start") {
		t.Error("prefix should still contain the received bytes even on no-content path")
	}
}

func TestReadUntilContent_ImmediateEOF_NoContent(t *testing.T) {
	prefix, hasContent := readUntilContent(strings.NewReader(""), firstContentCapBytes)
	if hasContent {
		t.Fatal("expected hasContent=false for immediate EOF (empty body)")
	}
	if len(prefix) != 0 {
		t.Errorf("expected empty prefix for immediate EOF, got %d bytes", len(prefix))
	}
}

func TestReadUntilContent_ManyPingsThenDelta_HasContent(t *testing.T) {
	// Upstream sends many pings before first content (e.g. slow thinking)
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("event: ping\ndata: {\"type\":\"ping\"}\n\n")
	}
	sb.WriteString("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n")

	prefix, hasContent := readUntilContent(strings.NewReader(sb.String()), firstContentCapBytes)
	if !hasContent {
		t.Fatal("expected hasContent=true after many pings then a content_block_delta")
	}
	if !strings.Contains(string(prefix), "content_block_delta") {
		t.Error("prefix should contain content_block_delta")
	}
}

func TestReadUntilContent_CapFallback_CommitsWithoutDelta(t *testing.T) {
	// If more than capBytes of preamble arrive with no delta, commit anyway (don't over-buffer).
	// Use a tiny cap so we can test with a short string.
	const smallCap = 20
	// This data is longer than smallCap and has no content_block_delta.
	data := strings.Repeat("x", 100)

	_, hasContent := readUntilContent(strings.NewReader(data), smallCap)
	if !hasContent {
		t.Fatal("expected hasContent=true when capBytes is exceeded (pathological long preamble)")
	}
}

func TestReadUntilContent_DeltaInFirstChunk_HasContent(t *testing.T) {
	// content_block_delta arrives immediately (fast upstream)
	sse := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\"}\n\n"
	_, hasContent := readUntilContent(strings.NewReader(sse), firstContentCapBytes)
	if !hasContent {
		t.Fatal("expected hasContent=true when delta is the very first event")
	}
}

// chunkedReader simulates an io.Reader that delivers data in small pieces,
// followed by an optional error (e.g. io.EOF) after all chunks are consumed.
type chunkedReader struct {
	chunks []string
	idx    int
	err    error
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if r.idx >= len(r.chunks) {
		if r.err != nil {
			return 0, r.err
		}
		return 0, io.EOF
	}
	n := copy(p, r.chunks[r.idx])
	r.idx++
	return n, nil
}

func TestReadUntilContent_SmallChunksThenDelta_HasContent(t *testing.T) {
	// Simulate upstream that trickles the stream in small chunks
	r := &chunkedReader{
		chunks: []string{
			"event: ping\ndata: {\"type\":\"ping\"}\n\n",
			"event: ping\ndata: {\"type\":\"ping\"}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\"}\n\n",
		},
		err: io.EOF,
	}

	_, hasContent := readUntilContent(r, firstContentCapBytes)
	if !hasContent {
		t.Fatal("expected hasContent=true when delta arrives in a later small chunk")
	}
}

func TestReadUntilContent_SmallChunksNoContent_NoContent(t *testing.T) {
	// Simulate upstream that sends preamble in chunks then EOF with no delta
	r := &chunkedReader{
		chunks: []string{
			"event: message_start\ndata: {}\n\n",
			"event: content_block_start\ndata: {}\n\n",
		},
		err: io.EOF,
	}

	_, hasContent := readUntilContent(r, firstContentCapBytes)
	if hasContent {
		t.Fatal("expected hasContent=false when chunks end before any content_block_delta")
	}
}
