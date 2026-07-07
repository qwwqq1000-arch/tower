package nodeclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/settings/api/proxy" {
			_, _ = w.Write([]byte(`{"proxy":"socks5://h:1:u:p","parsed":{"kind":"socks5"}}`))
			return
		}
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 500)
	}))
	defer srv.Close()
	pi, err := New(srv.URL, "k").GetProxy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pi.Proxy != "socks5://h:1:u:p" {
		t.Fatalf("proxy=%q", pi.Proxy)
	}
}

func TestTestProxySendsRaw(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/settings/api/proxy/test" {
			_ = json.NewDecoder(r.Body).Decode(&got)
			_, _ = w.Write([]byte(`{"ok":true,"egressIp":"1.2.3.4"}`))
			return
		}
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 500)
	}))
	defer srv.Close()
	out, err := New(srv.URL, "k").TestProxy(context.Background(), "socks5://h:1:u:p")
	if err != nil {
		t.Fatal(err)
	}
	if got["raw"] != "socks5://h:1:u:p" {
		t.Fatalf("raw=%q", got["raw"])
	}
	if out["ok"] != true {
		t.Fatalf("ok=%v", out["ok"])
	}
}

func TestSetProxySendsRawAndReturnsRestarting(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/settings/api/proxy" {
			_ = json.NewDecoder(r.Body).Decode(&got)
			_, _ = w.Write([]byte(`{"ok":true,"restarting":true}`))
			return
		}
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 500)
	}))
	defer srv.Close()
	out, err := New(srv.URL, "k").SetProxy(context.Background(), "socks5://h:1:u:p")
	if err != nil {
		t.Fatal(err)
	}
	if got["raw"] != "socks5://h:1:u:p" {
		t.Fatalf("raw=%q", got["raw"])
	}
	if out["restarting"] != true {
		t.Fatalf("restarting=%v", out["restarting"])
	}
}
