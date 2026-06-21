package dispatch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNodeProxy_OpenStream_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-app") != "cli" {
			t.Errorf("forge header missing")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: a\n\n")
		if fl != nil {
			fl.Flush()
		}
		_, _ = io.WriteString(w, "data: b\n\n")
	}))
	defer srv.Close()
	p := &NodeProxy{Body: []byte(`{"stream":true}`), Resolve: func(string) (NodeRef, bool) {
		return NodeRef{BaseURL: srv.URL, APIKey: "k", ProfileID: "default"}, true
	}, BanSignals: []int{401, 403}}
	st, err := p.OpenStream(context.Background(), "k")
	if err != nil || st.Status != 200 || st.Banned {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	defer st.Body.Close()
	data, _ := io.ReadAll(st.Body)
	if !strings.Contains(string(data), "data: a") || !strings.Contains(string(data), "data: b") {
		t.Fatalf("stream body=%q", data)
	}
}

func TestNodeProxy_OpenStream_BanByStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer srv.Close()
	p := &NodeProxy{Body: []byte(`{}`), Resolve: func(string) (NodeRef, bool) {
		return NodeRef{BaseURL: srv.URL, APIKey: "k"}, true
	}, BanSignals: []int{401, 403}}
	st, err := p.OpenStream(context.Background(), "k")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	defer st.Body.Close()
	if !st.Banned {
		t.Fatal("401 stream should be banned")
	}
}

func TestNodeProxy_OpenStream_UnknownKey(t *testing.T) {
	p := &NodeProxy{Body: []byte(`{}`), Resolve: func(string) (NodeRef, bool) { return NodeRef{}, false }}
	if _, err := p.OpenStream(context.Background(), "x"); err == nil {
		t.Fatal("unknown key should error")
	}
}
