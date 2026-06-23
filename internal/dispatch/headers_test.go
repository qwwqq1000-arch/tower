package dispatch

import (
	"net/http"
	"testing"
)

func TestStripHopByHop(t *testing.T) {
	h := http.Header{}
	h.Set("Transfer-Encoding", "chunked")
	h.Set("Connection", "keep-alive, X-Custom-Hop")
	h.Set("X-Custom-Hop", "v")
	h.Set("Content-Type", "application/json")
	StripHopByHop(h)
	for _, k := range []string{"Transfer-Encoding", "Connection", "X-Custom-Hop"} {
		if h.Get(k) != "" {
			t.Fatalf("%s should be stripped", k)
		}
	}
	if h.Get("Content-Type") != "application/json" {
		t.Fatal("Content-Type must survive")
	}
}

func TestForgeClaudeCodeHeaders(t *testing.T) {
	h := http.Header{}
	ForgeClaudeCodeHeaders(h)
	if h.Get("x-app") != "cli" {
		t.Fatalf("x-app=%q", h.Get("x-app"))
	}
	// Must NOT forge a fake claude-cli User-Agent — it is passed through to
	// Anthropic and tightens the account's rate limit; the node fills the real UA.
	if ua := h.Get("User-Agent"); ua != "" {
		t.Fatalf("User-Agent must not be forged, got %q", ua)
	}
}

func TestCopyForwardableHeaders(t *testing.T) {
	src := http.Header{}
	src.Set("Content-Type", "application/json")
	src.Set("X-Api-Key", "secret")
	src.Set("Authorization", "Bearer x")
	src.Set("Host", "evil")
	src.Set("Transfer-Encoding", "chunked")
	src.Set("Anthropic-Version", "2023-06-01")
	dst := http.Header{}
	CopyForwardableHeaders(dst, src)
	if dst.Get("Anthropic-Version") != "2023-06-01" {
		t.Fatal("safe header should be copied")
	}
	for _, k := range []string{"X-Api-Key", "Authorization", "Host", "Transfer-Encoding"} {
		if dst.Get(k) != "" {
			t.Fatalf("%s must NOT be copied", k)
		}
	}
}
