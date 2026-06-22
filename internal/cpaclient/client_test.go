package cpaclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListAccounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/auth-files" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer mgmt-secret" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"files":[
			{"id":"claude-a@gmail.com.json","auth_index":"idx1","name":"claude-a@gmail.com.json","provider":"claude","email":"a@gmail.com","account_type":"oauth","status":"active","disabled":false,"success":3,"failed":1},
			{"id":"claude-b@gmail.com.json","auth_index":"idx2","name":"claude-b@gmail.com.json","provider":"claude","email":"b@gmail.com","account_type":"oauth","status":"error","disabled":true,"success":0,"failed":5}
		]}`))
	}))
	defer srv.Close()

	accs, err := New(srv.URL, "mgmt-secret").ListAccounts(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(accs) != 2 {
		t.Fatalf("want 2 accounts, got %d", len(accs))
	}
	if accs[0].Email != "a@gmail.com" || accs[0].DispatchSelector() != "claude-a@gmail.com.json" {
		t.Errorf("acc0 = %+v", accs[0])
	}
	if statusFor(accs[0]) != "active" {
		t.Errorf("acc0 status = %s, want active", statusFor(accs[0]))
	}
	if statusFor(accs[1]) != "disabled" {
		t.Errorf("acc1 (disabled) status = %s, want disabled", statusFor(accs[1]))
	}
}

func TestListAccounts_AuthRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "wrong").ListAccounts(context.Background()); err == nil {
		t.Fatal("expected error on 401")
	}
}
