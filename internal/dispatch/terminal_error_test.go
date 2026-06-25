package dispatch

import (
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/policy"
)

// ─── terminalError ────────────────────────────────────────────────────────────

func TestTerminalError_MatchesInvalidRequest400(t *testing.T) {
	cfg := policy.Defaults()
	if !terminalError(400, `{"error":{"type":"invalid_request_error","message":"tools: Tool names must be unique"}}`, cfg) {
		t.Fatal("400 + invalid_request_error must be terminal")
	}
}

func TestTerminalError_Not400_NotTerminal(t *testing.T) {
	cfg := policy.Defaults()
	if terminalError(429, `{"error":{"type":"invalid_request_error"}}`, cfg) {
		t.Fatal("429 + keyword must NOT be terminal (wrong status)")
	}
}

func TestTerminalError_400_MissingKeyword_NotTerminal(t *testing.T) {
	cfg := policy.Defaults()
	if terminalError(400, `{"error":{"type":"rate_limit_error"}}`, cfg) {
		t.Fatal("400 without terminal keyword must NOT be terminal")
	}
}

func TestTerminalError_EmptyKeywords_NeverTerminal(t *testing.T) {
	cfg := policy.Defaults()
	cfg.TerminalErrorKeywords = []string{}
	if terminalError(400, `{"error":{"type":"invalid_request_error"}}`, cfg) {
		t.Fatal("empty keywords → feature off → never terminal")
	}
}

func TestTerminalError_CaseInsensitive(t *testing.T) {
	cfg := policy.Defaults()
	if !terminalError(400, `{"error":{"type":"INVALID_REQUEST_ERROR"}}`, cfg) {
		t.Fatal("match must be case-insensitive")
	}
}
