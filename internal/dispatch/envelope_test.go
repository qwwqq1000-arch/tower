package dispatch

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/policy"
)

func ccCfg() policy.Config {
	c := policy.Defaults()
	c.CCEnvelopeEnabled = true
	c.CCSystemPromptText = "You are Claude Code, Anthropic's official CLI for Claude."
	return c
}

func TestMissingEnvelopePieces(t *testing.T) {
	withSys := []byte(`{"system":"You are Claude Code, Anthropic's official CLI for Claude.\n\nrest"}`)
	noSys := []byte(`{"messages":[]}`)
	q := func(s string) url.Values { v, _ := url.ParseQuery(s); return v }
	hdr := func(ua, beta, xapp string) http.Header {
		h := http.Header{}
		if ua != "" { h.Set("User-Agent", ua) }
		if beta != "" { h.Set("anthropic-beta", beta) }
		if xapp != "" { h.Set("x-app", xapp) }
		return h
	}

	// All pieces off → nothing missing even when absent.
	c := ccCfg()
	if got := missingEnvelopePieces(c, noSys, q(""), hdr("", "", "")); got != nil {
		t.Fatalf("all-off should be nil, got %v", got)
	}

	// Only system-prompt piece on, prompt absent → just PieceSystemPrompt.
	c = ccCfg(); c.CCEnforceSystemPrompt = true
	if got := missingEnvelopePieces(c, noSys, q(""), hdr("", "", "")); len(got) != 1 || got[0] != PieceSystemPrompt {
		t.Fatalf("want [SystemPrompt], got %v", got)
	}
	// system present → none.
	if got := missingEnvelopePieces(c, withSys, q(""), hdr("", "", "")); got != nil {
		t.Fatalf("system present should be nil, got %v", got)
	}

	// Only beta piece on, beta absent → PieceBetaParam; present → none.
	c = ccCfg(); c.CCEnforceBetaParam = true
	if got := missingEnvelopePieces(c, noSys, q(""), hdr("", "", "")); len(got) != 1 || got[0] != PieceBetaParam {
		t.Fatalf("want [BetaParam], got %v", got)
	}
	if got := missingEnvelopePieces(c, noSys, q("beta=true"), hdr("", "", "")); got != nil {
		t.Fatalf("beta present should be nil, got %v", got)
	}

	// Only cli-headers on, headers absent → PieceCliHeaders; full set → none.
	c = ccCfg(); c.CCEnforceCliHeaders = true
	if got := missingEnvelopePieces(c, noSys, q(""), hdr("Go-http-client/1.1", "", "")); len(got) != 1 || got[0] != PieceCliHeaders {
		t.Fatalf("want [CliHeaders], got %v", got)
	}
	if got := missingEnvelopePieces(c, noSys, q(""), hdr("claude-cli/1.0", "oauth-2025-04-20", "cli")); got != nil {
		t.Fatalf("full cli headers should be nil, got %v", got)
	}
}

func TestInjectEnvelope(t *testing.T) {
	body := []byte(`{"model":"claude-opus-4-8","messages":[]}`)
	vals := envelopeVals{system: "You are Claude Code, Anthropic's official CLI for Claude.", ua: "claude-cli/1.0", beta: "oauth-2025-04-20", xapp: "cli"}
	miss := []EnvelopePiece{PieceSystemPrompt, PieceCliHeaders}

	h := http.Header{}
	newBody := injectEnvelope(miss, body, h, vals)

	if h.Get("User-Agent") != "claude-cli/1.0" || h.Get("x-app") != "cli" || h.Get("anthropic-beta") != "oauth-2025-04-20" {
		t.Fatalf("headers not injected: %v", h)
	}
	if !strings.Contains(string(newBody), "You are Claude Code") {
		t.Fatalf("system prompt not injected: %s", newBody)
	}
}

func TestInjectEnvelopeBadBodyUnchanged(t *testing.T) {
	body := []byte(`not json`)
	got := injectEnvelope([]EnvelopePiece{PieceSystemPrompt}, body, http.Header{}, envelopeVals{system: "x"})
	if string(got) != "not json" {
		t.Fatalf("bad body must be returned unchanged, got %s", got)
	}
}
