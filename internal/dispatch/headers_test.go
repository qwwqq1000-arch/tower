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
