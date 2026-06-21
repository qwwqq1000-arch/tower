package nodeclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginURLAndExchange(t *testing.T) {
	var exchBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login-url":
			_, _ = w.Write([]byte(`{"authorizeUrl":"https://claude.ai/x","codeVerifier":"v1","state":"s1"}`))
		case "/auth/exchange":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &exchBody)
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "k")
	lu, err := c.LoginURL(context.Background())
	if err != nil || lu.AuthorizeURL == "" || lu.CodeVerifier != "v1" || lu.State != "s1" {
		t.Fatalf("loginURL=%+v err=%v", lu, err)
	}
	if err := c.Exchange(context.Background(), "v1", "s1", "code123"); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if exchBody["code"] != "code123" || exchBody["codeVerifier"] != "v1" || exchBody["state"] != "s1" {
		t.Fatalf("exchange body=%+v", exchBody)
	}
}

func TestExchange_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }))
	defer srv.Close()
	if err := New(srv.URL, "k").Exchange(context.Background(), "v", "s", "c"); err == nil {
		t.Fatal("400 exchange should error")
	}
}

func TestProfilesList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"activeProfile":"default","profiles":[{"id":"default","type":"claude-max","email":"a@b.com","subscriptionType":"max","loggedIn":true}]}`))
	}))
	defer srv.Close()
	ps, err := New(srv.URL, "k").ProfilesList(context.Background())
	if err != nil || len(ps) != 1 || ps[0].Email != "a@b.com" || !ps[0].LoggedIn {
		t.Fatalf("profiles=%+v err=%v", ps, err)
	}
}
