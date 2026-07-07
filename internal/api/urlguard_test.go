package api

import "testing"

func TestValidateUpstreamURL(t *testing.T) {
	cases := []struct {
		name         string
		url          string
		allowPrivate bool
		wantErr      bool
	}{
		{"public https", "https://api.daodun.example.com/v1", false, false},
		{"public http with port", "http://203.0.113.10:8080", false, false},
		{"loopback ip", "http://127.0.0.1:8080", true, true},
		{"loopback name", "http://localhost/v1", true, true},
		{"metadata link-local", "http://169.254.169.254/latest/meta-data/", true, true},
		{"unspecified", "http://0.0.0.0/", true, true},
		{"private blocked for tenant", "http://10.1.2.3/v1", false, true},
		{"private allowed for admin node", "http://10.1.2.3/v1", true, false},
		{"private 192.168 blocked", "http://192.168.0.5/", false, true},
		{"bad scheme javascript", "javascript:alert(1)", true, true},
		{"bad scheme file", "file:///etc/passwd", true, true},
		{"empty host", "https://", false, true},
		{"garbage", "::::not a url", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateUpstreamURL(c.url, c.allowPrivate)
			if (err != nil) != c.wantErr {
				t.Fatalf("validateUpstreamURL(%q, %v) err=%v, wantErr=%v", c.url, c.allowPrivate, err, c.wantErr)
			}
		})
	}
}
