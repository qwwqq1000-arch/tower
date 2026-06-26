package dispatch

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// idleTimeoutReader wraps an io.ReadCloser and returns an error if no data
// is received within idleTimeout between consecutive reads. The underlying
// body is closed when the idle timeout fires or when Close is called.
//
// This prevents a silently-stalled upstream from holding a dispatch slot
// indefinitely (dispatch-core-6).
type idleTimeoutReader struct {
	src     io.ReadCloser
	timeout time.Duration

	pr *io.PipeReader
	pw *io.PipeWriter
}

// newIdleTimeoutReader wraps src so that each successive read must arrive within
// timeout. If the upstream stalls longer than timeout, the pipe is closed with
// an error and future reads return immediately.
func newIdleTimeoutReader(src io.ReadCloser, timeout time.Duration) *idleTimeoutReader {
	pr, pw := io.Pipe()
	r := &idleTimeoutReader{src: src, timeout: timeout, pr: pr, pw: pw}
	go r.pump()
	return r
}

func (r *idleTimeoutReader) pump() {
	buf := make([]byte, 32*1024)
	// timer fires if no data arrives within the idle window
	t := time.AfterFunc(r.timeout, func() {
		_ = r.pw.CloseWithError(fmt.Errorf("stream idle timeout after %v", r.timeout))
	})
	defer t.Stop()
	for {
		n, err := r.src.Read(buf)
		// Reset the idle timer after every successful read. Stop first to avoid
		// a race between a concurrent AfterFunc fire and the Reset call (Go timer
		// best practice for AfterFunc timers: Stop before Reset).
		if n > 0 {
			t.Stop()
			t.Reset(r.timeout)
		}
		if n > 0 {
			if _, werr := r.pw.Write(buf[:n]); werr != nil {
				// pipe reader closed (e.g. caller gave up) — stop pumping
				return
			}
		}
		if err != nil {
			_ = r.pw.CloseWithError(err)
			return
		}
	}
}

func (r *idleTimeoutReader) Read(p []byte) (int, error) { return r.pr.Read(p) }
func (r *idleTimeoutReader) Close() error {
	// Close both ends; pump goroutine will exit on next read error.
	_ = r.pr.Close()
	return r.src.Close()
}

func newHTTP(timeoutSec int) *http.Client {
	return &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
}

// readDecoded reads the upstream response body, transparently gunzipping it when
// the upstream compressed the response (Content-Encoding: gzip). The client's
// Accept-Encoding is forwarded verbatim (native passthrough), so Go's transport
// does NOT auto-decompress — Tower decodes here, like the direct cpa-key consumer
// would, both so it can parse the body and so it relays clean JSON. Without this,
// raw gzip is parsed as JSON → "invalid character '\x1f'" → 500.
func readDecoded(resp *http.Response) []byte {
	var r io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		if gz, err := gzip.NewReader(resp.Body); err == nil {
			defer gz.Close()
			r = gz
		}
	}
	data, _ := io.ReadAll(r)
	return data
}

// gzipBody decompresses a gzip stream and closes both the gzip reader and the
// underlying body on Close.
type gzipBody struct {
	gz  *gzip.Reader
	src io.ReadCloser
}

func (b *gzipBody) Read(p []byte) (int, error) { return b.gz.Read(p) }
func (b *gzipBody) Close() error               { _ = b.gz.Close(); return b.src.Close() }

// decodedStreamBody returns the response body, gunzipping it when the upstream
// compressed the stream, and drops the now-consumed Content-Encoding/Length so the
// decoded bytes are relayed to the client without a misleading gzip label.
func decodedStreamBody(resp *http.Response) io.ReadCloser {
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		if gz, err := gzip.NewReader(resp.Body); err == nil {
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
			return &gzipBody{gz: gz, src: resp.Body}
		}
	}
	return resp.Body
}

// msgURL builds the /v1/messages URL, tolerating a trailing slash on baseURL
// (a trailing slash would otherwise produce "...//v1/messages" → node 404).
func msgURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/v1/messages"
}

// NodeRef locates one account on a node.
type NodeRef struct {
	BaseURL     string
	APIKey      string
	ProfileID   string
	Kind        string // "meridian" (default) or "cpa"
	Passthrough bool   // cpa only: skip X-CLIProxy-Account, let CLIProxyAPI rotate
}

// setNodeAuthHeaders applies the per-kind auth/account-selection headers.
//   - meridian: x-api-key + x-meridian-profile (selects the profile on the node)
//   - cpa:      Authorization: Bearer + X-CLIProxy-Account (pins the account via
//     Tower's CPA fork)
func setNodeAuthHeaders(h http.Header, ref NodeRef) {
	if ref.Kind == "cpa" {
		if ref.APIKey != "" {
			h.Set("Authorization", "Bearer "+ref.APIKey)
		}
		if !ref.Passthrough && ref.ProfileID != "" {
			h.Set("X-CLIProxy-Account", ref.ProfileID)
		}
		return
	}
	h.Set("x-api-key", ref.APIKey)
	if ref.ProfileID != "" {
		h.Set("x-meridian-profile", ref.ProfileID)
	}
}

// Resolver maps an account key to its node connection info.
type Resolver func(key string) (NodeRef, bool)

// NodeProxy forwards a request to one of our nodes and classifies ban signals.
type NodeProxy struct {
	Body        []byte
	Resolve     Resolver
	BanSignals  []int
	BanKeywords []string
	HTTP        *http.Client
	// IdleTimeout, when non-zero, sets the maximum time between successive reads
	// from a streaming response body. A silently-stalled upstream triggers the
	// timeout and releases the dispatch slot (dispatch-core-6). Zero means no
	// idle timeout (the request context timeout governs instead).
	IdleTimeout time.Duration
	// UpstreamTimeoutSec is the total HTTP client timeout for upstream requests.
	// Zero falls back to the default of 300 seconds.
	UpstreamTimeoutSec int
	// EnvVals holds the cli config values used by envelope injection. Injection
	// only fires when envelopeInjectFrom(ctx) returns a non-empty miss set.
	EnvVals envelopeVals
}

func (p *NodeProxy) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	ts := p.UpstreamTimeoutSec
	if ts == 0 {
		ts = 300
	}
	return newHTTP(ts)
}

// Send proxies to {BaseURL}/v1/messages for the resolved account.
func (p *NodeProxy) Send(ctx context.Context, key string) (ProxyResult, error) {
	ref, ok := p.Resolve(key)
	if !ok {
		return ProxyResult{}, fmt.Errorf("unknown account %q", key)
	}
	miss := envelopeInjectFrom(ctx)
	u := msgURL(ref.BaseURL)
	if len(miss) > 0 && betaWanted(miss) {
		u += "?beta=true"
	}
	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(p.Body))
	if err != nil {
		return ProxyResult{}, err
	}
	// Pure passthrough: forward the client's original request headers verbatim
	// (CopyForwardableHeaders strips auth/host/length/hop-by-hop), then set only the
	// node's own auth + account pin. Nothing is forged — the upstream request is
	// identical to a direct cpa-key call, so CPA applies its normal cloak/fingerprint.
	CopyForwardableHeaders(req.Header, clientHeadersFrom(ctx))
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	setNodeAuthHeaders(req.Header, ref)
	if len(miss) > 0 {
		body := p.Body
		newBody := injectEnvelope(miss, body, req.Header, p.EnvVals)
		if len(newBody) != len(body) || string(newBody) != string(body) {
			req.Body = io.NopCloser(bytes.NewReader(newBody))
			req.ContentLength = int64(len(newBody))
		}
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return ProxyResult{}, err
	}
	defer resp.Body.Close()
	data := readDecoded(resp)
	body := string(data)
	status := resp.StatusCode
	// Claude can return a 200 header and then carry an error in the body (e.g.
	// {"type":"error","error":{"type":"overloaded_error"}}) — the same in-body
	// error the stream path catches via sseHasError. Demote a 2xx with an in-body
	// error to 500 so the orchestrator accounts it as an error and fails over
	// instead of reporting it as a clean success (ban classification on the body
	// is unchanged, so an in-body auth error still opens the breaker).
	if status >= 200 && status < 300 && sseHasError(body) {
		status = 500
	}
	return ProxyResult{
		Status: status,
		Body:   body,
		Banned: ClassifyBanned(resp.StatusCode, body, p.BanSignals, p.BanKeywords),
	}, nil
}

// ChannelRef locates an external fallback relay channel.
type ChannelRef struct {
	BaseURL string
	APIKey  string
}

// ChannelProxy forwards a request to a fallback channel. Fallback channels are
// external relays: no ban classification (Banned is always false).
type ChannelProxy struct {
	Body []byte
	Ch   ChannelRef
	HTTP *http.Client
	// IdleTimeout, when non-zero, sets the maximum time between successive reads
	// from a streaming response body (mirrors NodeProxy.IdleTimeout).
	IdleTimeout time.Duration
	// UpstreamTimeoutSec is the total HTTP client timeout for upstream requests.
	// Zero falls back to the default of 300 seconds.
	UpstreamTimeoutSec int
}

func (p *ChannelProxy) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	ts := p.UpstreamTimeoutSec
	if ts == 0 {
		ts = 300
	}
	return newHTTP(ts)
}

// Send proxies to {BaseURL}/v1/messages on the channel.
func (p *ChannelProxy) Send(ctx context.Context, _ string) (ProxyResult, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", msgURL(p.Ch.BaseURL), bytes.NewReader(p.Body))
	if err != nil {
		return ProxyResult{}, err
	}
	CopyForwardableHeaders(req.Header, clientHeadersFrom(ctx))
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("x-api-key", p.Ch.APIKey)
	resp, err := p.client().Do(req)
	if err != nil {
		return ProxyResult{}, err
	}
	defer resp.Body.Close()
	data := readDecoded(resp)
	return ProxyResult{Status: resp.StatusCode, Body: string(data), Banned: false}, nil
}

// Stream is an open streaming response from an upstream.
type Stream struct {
	Status int
	Header http.Header
	Body   io.ReadCloser
	Banned bool
}

// streamClient has no overall timeout (streaming responses are long-lived);
// cancellation is via the request context.
var streamClient = &http.Client{}

// OpenStream starts a streaming request to a node. The caller owns Body.
func (p *NodeProxy) OpenStream(ctx context.Context, key string) (*Stream, error) {
	ref, ok := p.Resolve(key)
	if !ok {
		return nil, fmt.Errorf("unknown account %q", key)
	}
	miss := envelopeInjectFrom(ctx)
	u := msgURL(ref.BaseURL)
	if len(miss) > 0 && betaWanted(miss) {
		u += "?beta=true"
	}
	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(p.Body))
	if err != nil {
		return nil, err
	}
	// Pure passthrough: forward the client's original request headers verbatim
	// (CopyForwardableHeaders strips auth/host/length/hop-by-hop), then set only the
	// node's own auth + account pin. Nothing is forged — the upstream request is
	// identical to a direct cpa-key call, so CPA applies its normal cloak/fingerprint.
	CopyForwardableHeaders(req.Header, clientHeadersFrom(ctx))
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	setNodeAuthHeaders(req.Header, ref)
	if len(miss) > 0 {
		body := p.Body
		newBody := injectEnvelope(miss, body, req.Header, p.EnvVals)
		if len(newBody) != len(body) || string(newBody) != string(body) {
			req.Body = io.NopCloser(bytes.NewReader(newBody))
			req.ContentLength = int64(len(newBody))
		}
	}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	src := decodedStreamBody(resp)
	body := src
	if p.IdleTimeout > 0 {
		body = newIdleTimeoutReader(src, p.IdleTimeout)
	}
	return &Stream{
		Status: resp.StatusCode,
		Header: resp.Header,
		Body:   body,
		Banned: ClassifyBanned(resp.StatusCode, "", p.BanSignals, nil),
	}, nil
}

// OpenStream starts a streaming request to a fallback channel (no ban classify).
func (p *ChannelProxy) OpenStream(ctx context.Context, _ string) (*Stream, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", msgURL(p.Ch.BaseURL), bytes.NewReader(p.Body))
	if err != nil {
		return nil, err
	}
	CopyForwardableHeaders(req.Header, clientHeadersFrom(ctx))
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("x-api-key", p.Ch.APIKey)
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	src := decodedStreamBody(resp)
	body := src
	if p.IdleTimeout > 0 {
		body = newIdleTimeoutReader(src, p.IdleTimeout)
	}
	return &Stream{Status: resp.StatusCode, Header: resp.Header, Body: body, Banned: false}, nil
}
