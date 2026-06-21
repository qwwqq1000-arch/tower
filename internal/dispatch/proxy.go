package dispatch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

func newHTTP() *http.Client { return &http.Client{Timeout: 300 * time.Second} }

// NodeRef locates one account on a node.
type NodeRef struct {
	BaseURL   string
	APIKey    string
	ProfileID string
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
	req, err := http.NewRequestWithContext(ctx, "POST", ref.BaseURL+"/v1/messages", bytes.NewReader(p.Body))
	if err != nil {
		return ProxyResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", ref.APIKey)
	if ref.ProfileID != "" {
		req.Header.Set("x-meridian-profile", ref.ProfileID)
	}
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
	req, err := http.NewRequestWithContext(ctx, "POST", p.Ch.BaseURL+"/v1/messages", bytes.NewReader(p.Body))
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
