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
