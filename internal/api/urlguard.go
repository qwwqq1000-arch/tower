package api

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// validateUpstreamURL guards a user/admin-supplied base URL that Tower will later
// proxy to (SSRF defense — security-audit). It requires an http(s) scheme and a
// host, and rejects hosts that are (or resolve to) loopback, link-local — which
// includes the cloud-metadata address 169.254.169.254 — or the unspecified
// address. When allowPrivate is false (untrusted tenant input, e.g. a fallback
// channel a tenant can set) it additionally rejects RFC1918 / unique-local
// private ranges so a tenant cannot make Tower reach internal services.
//
// This is a write-time guard: it blocks the obvious literal-IP and well-known
// hostname vectors. It is best-effort against DNS targets (a non-resolving host
// passes) and is not a substitute for dial-time IP filtering against DNS
// rebinding — that is a deeper follow-up. It exists to stop casual SSRF config.
func validateUpstreamURL(raw string, allowPrivate bool) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url host required")
	}
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("url host not allowed (loopback)")
	}
	// Resolve candidate IPs: a literal IP, or the A/AAAA records for a hostname.
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else if resolved, rerr := net.LookupIP(host); rerr == nil {
		ips = resolved
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("url host not allowed (loopback/link-local/metadata)")
		}
		if !allowPrivate && ip.IsPrivate() {
			return fmt.Errorf("url host not allowed (private range)")
		}
	}
	return nil
}
