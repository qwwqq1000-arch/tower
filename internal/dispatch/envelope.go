package dispatch

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/qwwqq1000-arch/tower/internal/policy"
)

// envelopeVals holds the cli config values needed for injection.
type envelopeVals struct{ system, ua, beta, xapp string }

// betaWanted reports whether PieceBetaParam is in the miss set.
func betaWanted(miss []EnvelopePiece) bool {
	for _, m := range miss {
		if m == PieceBetaParam {
			return true
		}
	}
	return false
}

// injectEnvelope sets the missing cli headers on h and prepends the system prompt to
// body for the requested pieces. Best-effort: a body that is not JSON is returned
// unchanged. (BetaParam is applied at the URL, not here.)
func injectEnvelope(miss []EnvelopePiece, body []byte, h http.Header, v envelopeVals) []byte {
	want := func(p EnvelopePiece) bool {
		for _, m := range miss {
			if m == p {
				return true
			}
		}
		return false
	}
	if want(PieceCliHeaders) {
		if v.ua != "" {
			h.Set("User-Agent", v.ua)
		}
		if v.beta != "" {
			h.Set("anthropic-beta", v.beta)
		}
		if v.xapp != "" {
			h.Set("x-app", v.xapp)
		}
	}
	if want(PieceSystemPrompt) && v.system != "" {
		var m map[string]json.RawMessage
		if json.Unmarshal(body, &m) == nil {
			raw, hasSys := m["system"]
			// Array-of-blocks form: unshift a text block, preserving existing content.
			if hasSys {
				var blocks []json.RawMessage
				if json.Unmarshal(raw, &blocks) == nil {
					already := false
					for _, b := range blocks {
						var blk struct {
							Text string `json:"text"`
						}
						if json.Unmarshal(b, &blk) == nil && strings.Contains(blk.Text, v.system) {
							already = true
							break
						}
					}
					if already {
						return body
					}
					if nbk, err := json.Marshal(map[string]string{"type": "text", "text": v.system}); err == nil {
						blocks = append([]json.RawMessage{json.RawMessage(nbk)}, blocks...)
						if enc, err := json.Marshal(blocks); err == nil {
							m["system"] = enc
							if nb, err := json.Marshal(m); err == nil {
								return nb
							}
						}
					}
					return body
				}
			}
			// String form (or system absent): prepend as a string.
			var existing string
			if hasSys {
				_ = json.Unmarshal(raw, &existing)
			}
			combined := v.system
			if existing != "" && !strings.Contains(existing, v.system) {
				combined = v.system + "\n\n" + existing
			}
			if enc, err := json.Marshal(combined); err == nil {
				m["system"] = enc
				if nb, err := json.Marshal(m); err == nil {
					return nb
				}
			}
		}
	}
	return body
}

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
