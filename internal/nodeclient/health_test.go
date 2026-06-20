package nodeclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "k1" {
			t.Errorf("missing api key header")
		}
		_, _ = w.Write([]byte(`{"status":"healthy","version":"1.45.0","mode":"internal","auth":{"loggedIn":true,"email":"a@b.com","subscriptionType":"max"}}`))
	}))
	defer srv.Close()

	h, err := New(srv.URL, "k1").Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "healthy" || h.Version != "1.45.0" || !h.Auth.LoggedIn || h.Auth.Email != "a@b.com" || h.Auth.SubscriptionType != "max" {
		t.Fatalf("health = %+v", h)
	}
}

func TestQuotaAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/usage/quota/all" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"activeProfile":"default","profiles":[{"id":"default","isActive":true,"windows":[{"type":"five_hour","status":"allowed_warning","utilization":0.9,"resetsAt":1781987399166}]}]}`))
	}))
	defer srv.Close()

	q, err := New(srv.URL, "k1").QuotaAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if q.ActiveProfile != "default" || len(q.Profiles) != 1 || q.Profiles[0].Windows[0].Type != "five_hour" || q.Profiles[0].Windows[0].Utilization != 0.9 {
		t.Fatalf("quota = %+v", q)
	}
}

func TestHealth_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "k").Health(context.Background()); err == nil {
		t.Fatal("expected error on 500")
	}
}
