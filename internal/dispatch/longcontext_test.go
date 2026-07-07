package dispatch

import (
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/policy"
)

func cfgLC() policy.Config {
	c := policy.Defaults()
	c.LongContextGateEnabled = true
	return c
}

func TestIsLongContextRequest(t *testing.T) {
	c := cfgLC() // threshold 200000 tokens, markers ["1m"]
	big := []byte(strings.Repeat("x", 200001*4+8))
	if !isLongContextRequest("claude-sonnet-4-6", big, c) {
		t.Fatal("oversized body should be long-context by token estimate")
	}
	if isLongContextRequest("claude-sonnet-4-6", []byte(`{"a":1}`), c) {
		t.Fatal("small body, no marker → not long")
	}
	if !isLongContextRequest("claude-sonnet-4-6[1M]", []byte(`{"a":1}`), c) {
		t.Fatal("model marker (case-insensitive) → long")
	}
	c.LongContextTokenThreshold = 0 // disable token path
	if isLongContextRequest("claude-sonnet-4-6", big, c) {
		t.Fatal("threshold 0 → token path off")
	}
	if !isLongContextRequest("x-1m", big, c) {
		t.Fatal("threshold 0 but marker present → long")
	}
}

func TestIsExtraUsageNo1M(t *testing.T) {
	c := cfgLC()
	body := `{"type":"error","error":{"message":"Third-party apps now draw from your external/extra usage ..."}}`
	if !isExtraUsageNo1M(400, body, c) {
		t.Fatal("400 + keyword should match")
	}
	if isExtraUsageNo1M(429, body, c) {
		t.Fatal("non-400 status should not match")
	}
	if isExtraUsageNo1M(400, `{"error":"rate limited"}`, c) {
		t.Fatal("400 without keyword should not match")
	}
}
