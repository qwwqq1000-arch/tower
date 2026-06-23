package dispatch

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestNodeProxy_Send_ForgesClaudeCodeHeaders(t *testing.T) {
	var gotUA, gotApp string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotApp = r.Header.Get("x-app")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	resolve := func(key string) (NodeRef, bool) {
		return NodeRef{BaseURL: srv.URL, APIKey: "k1", ProfileID: "default", Kind: "meridian"}, true
	}
	p := &NodeProxy{Body: []byte(`{"model":"opus"}`), Resolve: resolve}

	_, err := p.Send(context.Background(), "node1:default")
	if err != nil {
		t.Fatalf("send error: %v", err)
	}
	if gotUA == "" {
		t.Fatalf("User-Agent not set by Send (got empty)")
	}
	if gotApp != "cli" {
		t.Fatalf("x-app=%q, want cli", gotApp)
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
