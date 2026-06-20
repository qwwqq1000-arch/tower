package nodeclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetFeatures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"opencode":{"memory":false,"thinking":"disabled"}}`))
	}))
	defer srv.Close()
	f, err := New(srv.URL, "k").GetFeatures(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if f["opencode"]["thinking"] != "disabled" {
		t.Fatalf("features = %+v", f)
	}
}

func TestPatchFeatures(t *testing.T) {
	var gotBody map[string]any
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	err := New(srv.URL, "k").PatchFeatures(context.Background(), "opencode", map[string]any{"memory": true})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/settings/api/features/opencode" || gotBody["memory"] != true {
		t.Fatalf("path=%s body=%+v", gotPath, gotBody)
	}
}

func TestAuthImport_SendsProfileHeaderAndBody(t *testing.T) {
	var gotProfile string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/import" {
			t.Errorf("path = %s", r.URL.Path)
		}
		gotProfile = r.Header.Get("x-meridian-profile")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()
	err := New(srv.URL, "k").AuthImport(context.Background(), "work", ImportCreds{Access: "a", Refresh: "r", ExpiresAt: 123, Email: "e@x.com"})
	if err != nil {
		t.Fatal(err)
	}
	if gotProfile != "work" || gotBody["access"] != "a" || gotBody["email"] != "e@x.com" {
		t.Fatalf("profile=%s body=%+v", gotProfile, gotBody)
	}
}
