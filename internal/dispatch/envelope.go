package dispatch

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/qwwqq1000-arch/tower/internal/policy"
)

type EnvelopePiece int

const (
	PieceSystemPrompt EnvelopePiece = iota
	PieceBetaParam
	PieceCliHeaders
)

// missingEnvelopePieces returns the ENABLED three-piece-set pieces absent from the
// request. Only pieces whose toggle is on are checked; returns nil when nothing
// enabled is missing (the common path).
func missingEnvelopePieces(cfg policy.Config, body []byte, q url.Values, h http.Header) []EnvelopePiece {
	if !cfg.CCEnvelopeEnabled {
		return nil
	}
	var miss []EnvelopePiece
	if cfg.CCEnforceSystemPrompt && !bodyHasSystemPrompt(body, cfg.CCSystemPromptText) {
		miss = append(miss, PieceSystemPrompt)
	}
	if cfg.CCEnforceBetaParam && q.Get("beta") != "true" {
		miss = append(miss, PieceBetaParam)
	}
	if cfg.CCEnforceCliHeaders {
		ua := h.Get("User-Agent")
		if !strings.HasPrefix(strings.ToLower(ua), "claude-cli") || h.Get("anthropic-beta") == "" || h.Get("x-app") == "" {
			miss = append(miss, PieceCliHeaders)
		}
	}
	return miss
}

// bodyHasSystemPrompt reports whether the request body's system field contains want.
// system may be a string or an array of {type:"text",text:...} blocks.
func bodyHasSystemPrompt(body []byte, want string) bool {
	if want == "" {
		return true
	}
	var probe struct {
		System json.RawMessage `json:"system"`
	}
	if json.Unmarshal(body, &probe) != nil || len(probe.System) == 0 {
		return false
	}
	var asStr string
	if json.Unmarshal(probe.System, &asStr) == nil {
		return strings.Contains(asStr, want)
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(probe.System, &blocks) == nil {
		for _, b := range blocks {
			if strings.Contains(b.Text, want) {
				return true
			}
		}
	}
	return false
}
