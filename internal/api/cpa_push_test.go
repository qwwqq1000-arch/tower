package api

import "testing"

func TestNormalizeNodeBaseURL(t *testing.T) {
	cases := []struct{ in, baseURL, host string }{
		{"147.90.235.170", "http://147.90.235.170:8080/", "147.90.235.170:8080"},
		{"147.90.235.170:8317", "http://147.90.235.170:8317/", "147.90.235.170:8317"},
		{"http://1.2.3.4:8080/", "http://1.2.3.4:8080/", "1.2.3.4:8080"},
		{"  10.0.0.5  ", "http://10.0.0.5:8080/", "10.0.0.5:8080"},
	}
	for _, c := range cases {
		b, h, err := normalizeNodeBaseURL(c.in)
		if err != nil || b != c.baseURL || h != c.host {
			t.Fatalf("in=%q → (%q,%q,%v), want (%q,%q)", c.in, b, h, err, c.baseURL, c.host)
		}
	}
	if _, _, err := normalizeNodeBaseURL("   "); err == nil {
		t.Fatal("expected error on empty url")
	}
}

func TestNodeHostPort(t *testing.T) {
	if h := nodeHostPort("http://147.90.235.170:8080/"); h != "147.90.235.170:8080" {
		t.Fatalf("host=%q", h)
	}
	if h := nodeHostPort(""); h != "" {
		t.Fatalf("empty base_url should give empty host, got %q", h)
	}
}
