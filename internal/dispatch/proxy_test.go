package dispatch

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNodeProxy_SendOKAndBan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "k1" {
			t.Errorf("missing api key")
		}
		if r.Header.Get("x-meridian-profile") != "default" {
			t.Errorf("missing profile header")
		}
		if r.Header.Get("x-ban") == "1" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	resolve := func(key string) (NodeRef, bool) {
		return NodeRef{BaseURL: srv.URL, APIKey: "k1", ProfileID: "default"}, true
	}
	p := &NodeProxy{Body: []byte(`{"model":"opus"}`), Resolve: resolve, BanSignals: []int{401, 403}}

	res, err := p.Send(context.Background(), "node1:default")
	if err != nil || res.Status != 200 || res.Banned {
		t.Fatalf("ok send: res=%+v err=%v", res, err)
	}
}

func TestNodeProxy_UnknownKey(t *testing.T) {
	p := &NodeProxy{Body: []byte(`{}`), Resolve: func(string) (NodeRef, bool) { return NodeRef{}, false }}
	if _, err := p.Send(context.Background(), "x"); err == nil {
		t.Fatal("unknown key should error")
	}
}

func TestNodeProxy_BanByStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer srv.Close()
	p := &NodeProxy{Body: []byte(`{}`), Resolve: func(string) (NodeRef, bool) {
		return NodeRef{BaseURL: srv.URL, APIKey: "k"}, true
	}, BanSignals: []int{401, 403}}
	res, err := p.Send(context.Background(), "k")
	if err != nil || !res.Banned {
		t.Fatalf("403 should be banned: res=%+v err=%v", res, err)
	}
}

func TestNodeProxy_SendInBodyErrorOn200(t *testing.T) {
	// Claude can return a 200 header and then carry an error in the body
	// (e.g. {"type":"error","error":{"type":"overloaded_error"}}). The
	// non-stream path must treat this as an error (so the orchestrator fails
	// over), mirroring the stream path's sseHasError handling.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	}))
	defer srv.Close()
	p := &NodeProxy{Body: []byte(`{}`), Resolve: func(string) (NodeRef, bool) {
		return NodeRef{BaseURL: srv.URL, APIKey: "k"}, true
	}, BanSignals: []int{401, 403}}
	res, err := p.Send(context.Background(), "k")
	if err != nil {
		t.Fatalf("send error: %v", err)
	}
	if res.Status >= 200 && res.Status < 300 {
		t.Fatalf("in-body error on 200 must be demoted to a non-2xx status, got %d", res.Status)
	}
	if res.Banned {
		t.Fatalf("overloaded_error is transient, not a ban: res=%+v", res)
	}
}

func TestNodeProxy_SendInBodyBanOn200(t *testing.T) {
	// An in-body error whose body matches a ban keyword (e.g. an auth error
	// returned with a 200 header) is a real ban — classify it accordingly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer srv.Close()
	p := &NodeProxy{Body: []byte(`{}`), Resolve: func(string) (NodeRef, bool) {
		return NodeRef{BaseURL: srv.URL, APIKey: "k"}, true
	}, BanSignals: []int{401, 403}, BanKeywords: []string{"authentication_error"}}
	res, err := p.Send(context.Background(), "k")
	if err != nil {
		t.Fatalf("send error: %v", err)
	}
	if res.Status >= 200 && res.Status < 300 {
		t.Fatalf("in-body ban on 200 must be demoted to a non-2xx status, got %d", res.Status)
	}
	if !res.Banned {
		t.Fatalf("authentication_error in body should be classified as banned: res=%+v", res)
	}
}

func TestNodeProxy_Send_PassesThroughClientHeaders(t *testing.T) {
	// Pure passthrough: the client's original headers (anthropic-version, its own
	// User-Agent) reach the upstream verbatim; Tower adds NO x-app / forged UA and
	// re-sets only the node auth (the client's Authorization is stripped).
	var gotUA, gotApp, gotVer, gotKey, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotApp = r.Header.Get("x-app")
		gotVer = r.Header.Get("Anthropic-Version")
		gotKey = r.Header.Get("x-api-key")
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	resolve := func(key string) (NodeRef, bool) {
		return NodeRef{BaseURL: srv.URL, APIKey: "k1", ProfileID: "default", Kind: "meridian"}, true
	}
	p := &NodeProxy{Body: []byte(`{"model":"opus"}`), Resolve: resolve}

	client := http.Header{}
	client.Set("Anthropic-Version", "2023-06-01")
	client.Set("User-Agent", "newapi/1.0")
	client.Set("Authorization", "Bearer dk_client_should_be_stripped")
	ctx := WithClientHeaders(context.Background(), client)

	if _, err := p.Send(ctx, "node1:default"); err != nil {
		t.Fatalf("send error: %v", err)
	}
	if gotVer != "2023-06-01" {
		t.Fatalf("Anthropic-Version not forwarded, got %q", gotVer)
	}
	if gotUA != "newapi/1.0" {
		t.Fatalf("client User-Agent not forwarded verbatim, got %q", gotUA)
	}
	if gotApp != "" {
		t.Fatalf("x-app must not be added (pure passthrough), got %q", gotApp)
	}
	if gotKey != "k1" {
		t.Fatalf("node x-api-key not set, got %q", gotKey)
	}
	if strings.Contains(gotAuth, "dk_client") {
		t.Fatalf("client Authorization must be stripped, got %q", gotAuth)
	}
}

func TestNodeProxy_Send_CPA_Passthrough(t *testing.T) {
	// CPA: re-set only the Bearer node key + the X-CLIProxy-Account pin, forward the
	// client headers verbatim, and add NOTHING else (no x-app) — identical to a
	// direct cpa-key call, so CPA applies its own cloak/fingerprint.
	var gotApp, gotAuth, gotAcct, gotVer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotApp = r.Header.Get("x-app")
		gotAuth = r.Header.Get("Authorization")
		gotAcct = r.Header.Get("X-CLIProxy-Account")
		gotVer = r.Header.Get("Anthropic-Version")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	resolve := func(key string) (NodeRef, bool) {
		return NodeRef{BaseURL: srv.URL, APIKey: "sk-node", ProfileID: "acct.json", Kind: "cpa"}, true
	}
	p := &NodeProxy{Body: []byte(`{"model":"opus"}`), Resolve: resolve}
	client := http.Header{}
	client.Set("Anthropic-Version", "2023-06-01")
	ctx := WithClientHeaders(context.Background(), client)

	if _, err := p.Send(ctx, "n:acct"); err != nil {
		t.Fatalf("send error: %v", err)
	}
	if gotAuth != "Bearer sk-node" {
		t.Fatalf("Authorization=%q, want Bearer sk-node", gotAuth)
	}
	if gotAcct != "acct.json" {
		t.Fatalf("X-CLIProxy-Account=%q, want acct.json", gotAcct)
	}
	if gotVer != "2023-06-01" {
		t.Fatalf("Anthropic-Version not forwarded, got %q", gotVer)
	}
	if gotApp != "" {
		t.Fatalf("x-app must not be added (pure passthrough), got %q", gotApp)
	}
}

func TestNodeProxy_Send_GzipResponseDecoded(t *testing.T) {
	// Regression: with Accept-Encoding forwarded (native passthrough), Go hands back
	// raw gzip bytes; parsing them as JSON yields "invalid character '\x1f'" → 500.
	// readDecoded must gunzip the upstream response so Send returns clean JSON.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write([]byte(`{"ok":true,"usage":{"input_tokens":5}}`))
		_ = gz.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	p := &NodeProxy{Body: []byte(`{}`), Resolve: func(string) (NodeRef, bool) {
		return NodeRef{BaseURL: srv.URL, APIKey: "k"}, true
	}}
	// Client forwards Accept-Encoding: gzip — Tower must decode the response itself.
	ctx := WithClientHeaders(context.Background(), http.Header{"Accept-Encoding": {"gzip"}})
	res, err := p.Send(ctx, "k")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.Contains(res.Body, `"input_tokens":5`) {
		t.Fatalf("gzip response not decoded, got: %q", res.Body)
	}
}

func TestChannelProxy_Send(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"relayed":true}`))
	}))
	defer srv.Close()
	p := &ChannelProxy{Body: []byte(`{}`), Ch: ChannelRef{BaseURL: srv.URL, APIKey: "ck"}}
	res, err := p.Send(context.Background(), "ch1")
	if err != nil || res.Status != 200 || res.Banned {
		t.Fatalf("channel send: res=%+v err=%v", res, err)
	}
}
