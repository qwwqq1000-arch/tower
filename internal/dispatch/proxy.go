package dispatch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func newHTTP() *http.Client { return &http.Client{Timeout: 300 * time.Second} }

// msgURL builds the /v1/messages URL, tolerating a trailing slash on baseURL
// (a trailing slash would otherwise produce "...//v1/messages" → node 404).
func msgURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/v1/messages"
}

// NodeRef locates one account on a node.
type NodeRef struct {
	BaseURL   string
	APIKey    string
	ProfileID string
	Kind      string // "meridian" (default) or "cpa"
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
		if ref.ProfileID != "" {
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
}

func (p *NodeProxy) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return newHTTP()
}

// Send proxies to {BaseURL}/v1/messages for the resolved account.
func (p *NodeProxy) Send(ctx context.Context, key string) (ProxyResult, error) {
	ref, ok := p.Resolve(key)
	if !ok {
		return ProxyResult{}, fmt.Errorf("unknown account %q", key)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", msgURL(ref.BaseURL), bytes.NewReader(p.Body))
	if err != nil {
		return ProxyResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	setNodeAuthHeaders(req.Header, ref)
	resp, err := p.client().Do(req)
	if err != nil {
		return ProxyResult{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	body := string(data)
	return ProxyResult{
		Status: resp.StatusCode,
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
}

func (p *ChannelProxy) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return newHTTP()
}

// Send proxies to {BaseURL}/v1/messages on the channel.
func (p *ChannelProxy) Send(ctx context.Context, _ string) (ProxyResult, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", msgURL(p.Ch.BaseURL), bytes.NewReader(p.Body))
	if err != nil {
		return ProxyResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.Ch.APIKey)
	resp, err := p.client().Do(req)
	if err != nil {
		return ProxyResult{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
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
	req, err := http.NewRequestWithContext(ctx, "POST", msgURL(ref.BaseURL), bytes.NewReader(p.Body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	setNodeAuthHeaders(req.Header, ref)
	ForgeClaudeCodeHeaders(req.Header)
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	return &Stream{
		Status: resp.StatusCode,
		Header: resp.Header,
		Body:   resp.Body,
		Banned: ClassifyBanned(resp.StatusCode, "", p.BanSignals, nil),
	}, nil
}

// OpenStream starts a streaming request to a fallback channel (no ban classify).
func (p *ChannelProxy) OpenStream(ctx context.Context, _ string) (*Stream, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", msgURL(p.Ch.BaseURL), bytes.NewReader(p.Body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.Ch.APIKey)
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	return &Stream{Status: resp.StatusCode, Header: resp.Header, Body: resp.Body, Banned: false}, nil
}
