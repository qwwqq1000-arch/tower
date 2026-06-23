package dispatch

import (
	"net/http"
	"strings"
)

var hopByHop = map[string]bool{
	"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true,
	"Proxy-Authorization": true, "Te": true, "Trailer": true,
	"Transfer-Encoding": true, "Upgrade": true,
}

// StripHopByHop removes hop-by-hop headers in place, including any named by the
// Connection header (RFC 7230).
func StripHopByHop(h http.Header) {
	for _, v := range h.Values("Connection") {
		for _, name := range strings.Split(v, ",") {
			if n := strings.TrimSpace(name); n != "" {
				h.Del(n)
			}
		}
	}
	for k := range hopByHop {
		h.Del(k)
	}
}

// ForgeClaudeCodeHeaders marks the request as a Claude Code client via x-app. It
// deliberately does NOT set a User-Agent: a fake/outdated "claude-cli/1.0" UA is
// passed through verbatim by CLIProxyAPI to Anthropic, which then flags the
// account as a suspicious client and tightens its rate limit (real 429 + account
// cooldown that then 429s every later request). Leaving the UA unset lets the
// node fill in the correct per-profile fingerprint — dispatch passes client
// identity through, it does not forge it.
func ForgeClaudeCodeHeaders(h http.Header) {
	h.Set("x-app", "cli")
}

var noCopy = map[string]bool{
	"Host": true, "Content-Length": true, "X-Api-Key": true, "Authorization": true,
}

// CopyForwardableHeaders copies src→dst, skipping hop-by-hop and auth/length
// headers the proxy re-sets itself.
func CopyForwardableHeaders(dst, src http.Header) {
	for k, vs := range src {
		ck := http.CanonicalHeaderKey(k)
		if hopByHop[ck] || noCopy[ck] {
			continue
		}
		for _, v := range vs {
			dst.Add(ck, v)
		}
	}
}
