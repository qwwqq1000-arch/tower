package dispatch

import (
	"context"
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

// clientHeadersKeyT is the context key for the original downstream request headers.
type clientHeadersKeyT struct{}

// WithClientHeaders stashes the original downstream (new-api) request headers on
// ctx so the proxy can forward them verbatim to the upstream node/channel — pure
// passthrough: the upstream sees the same request a direct cpa-key call would send,
// adding nothing (no x-app, no forged User-Agent), so CPA applies its own cloak.
func WithClientHeaders(ctx context.Context, h http.Header) context.Context {
	return context.WithValue(ctx, clientHeadersKeyT{}, h)
}

// clientHeadersFrom returns the headers stashed by WithClientHeaders, or nil.
// CopyForwardableHeaders tolerates a nil source (copies nothing).
func clientHeadersFrom(ctx context.Context) http.Header {
	h, _ := ctx.Value(clientHeadersKeyT{}).(http.Header)
	return h
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
