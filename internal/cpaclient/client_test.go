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

// TestUsage_ApiCall tests the new CPA v7.2.40+ api-call path.
func TestUsage_ApiCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v0/management/api-call" {
			w.Header().Set("Content-Type", "application/json")
			// The body field is a JSON string (the outer envelope wraps the inner JSON as a string).
			_, _ = w.Write([]byte(`{"status_code":200,"header":{},` +
				`"body":"{\"five_hour\":{\"utilization\":8.0,\"resets_at\":\"2026-06-25T21:00:00+00:00\"},` +
				`\"seven_day\":{\"utilization\":6.0,\"resets_at\":\"2026-06-29T00:00:00+00:00\"},` +
				`\"seven_day_sonnet\":{\"utilization\":0.0,\"resets_at\":null},\"seven_day_opus\":null}"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	u, err := New(srv.URL, "k").Usage(context.Background(), "18ab29ec4dc98d68", "claude-a@gmail.com.json")
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if u.FiveHour == nil || u.FiveHour.Utilization != 8.0 {
		t.Fatalf("five_hour = %+v, want 8.0", u.FiveHour)
	}
	if u.SevenDay == nil || u.SevenDay.Utilization != 6.0 {
		t.Fatalf("seven_day = %+v, want 6.0", u.SevenDay)
	}
	if u.SevenDaySonnet == nil || u.SevenDaySonnet.Utilization != 0.0 {
		t.Fatalf("seven_day_sonnet = %+v, want 0.0", u.SevenDaySonnet)
	}
	if u.SevenDayOpus != nil {
		t.Fatalf("seven_day_opus should be nil")
	}
}

// TestUsage_ApiCall_FallbackOnNotFound tests that a 404 on /api-call falls back to legacy account-usage.
func TestUsage_ApiCall_FallbackOnNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v0/management/api-call" {
			http.NotFound(w, r)
			return
		}
		if r.Method == "GET" && r.URL.Path == "/v0/management/account-usage" {
			_, _ = w.Write([]byte(`{"five_hour":{"utilization":0.0,"resets_at":"2026-06-22T17:20:00Z"},"seven_day":{"utilization":53.0,"resets_at":"2026-06-26T12:00:00Z"},"seven_day_opus":null,"seven_day_sonnet":{"utilization":8.0,"resets_at":"2026-06-26T12:00:00Z"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	u, err := New(srv.URL, "k").Usage(context.Background(), "idx1", "claude-a@gmail.com.json")
	if err != nil {
		t.Fatalf("usage fallback: %v", err)
	}
	if u.SevenDay == nil || u.SevenDay.Utilization != 53.0 {
		t.Fatalf("seven_day = %+v, want 53", u.SevenDay)
	}
	if u.SevenDaySonnet == nil || u.SevenDaySonnet.Utilization != 8.0 {
		t.Fatalf("seven_day_sonnet = %+v, want 8", u.SevenDaySonnet)
	}
	if u.SevenDayOpus != nil {
		t.Fatalf("seven_day_opus should be nil")
	}
}

// TestUsage_ApiCall_UpstreamError tests that a non-200 inner status_code returns an error.
func TestUsage_ApiCall_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v0/management/api-call" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status_code":429,"header":{},"body":"rate limited"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, err := New(srv.URL, "k").Usage(context.Background(), "idx1", "claude-a@gmail.com.json")
	if err == nil {
		t.Fatal("expected error on upstream 429, got nil")
	}
}
