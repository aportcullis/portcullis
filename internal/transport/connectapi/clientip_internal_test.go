package connectapi

// White-box: clientIP's trusted-proxy walk and its malformed-hop fallback live in
// unexported helpers; a black-box e2e request can't set an arbitrary peer address.

import (
	"net"
	"net/http"
	"strings"
	"testing"
)

func mustNets(t *testing.T, cidrs ...string) []*net.IPNet {
	t.Helper()
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			t.Fatalf("parse cidr %q: %v", c, err)
		}
		nets = append(nets, n)
	}
	return nets
}

func xff(values ...string) http.Header {
	h := http.Header{}
	for _, v := range values {
		h.Add("X-Forwarded-For", v)
	}
	return h
}

func TestClientIP(t *testing.T) {
	t.Parallel()
	trusted := mustNets(t, "10.0.0.0/8")

	cases := []struct {
		name    string
		addr    string
		header  http.Header
		trusted []*net.IPNet
		want    string
	}{
		{"no trusted proxies uses peer", "203.0.113.5:443", xff("1.2.3.4"), nil, "203.0.113.5"},
		{"untrusted peer ignores xff", "203.0.113.5:443", xff("1.2.3.4"), trusted, "203.0.113.5"},
		{"trusted proxy returns client", "10.0.0.1:443", xff("198.51.100.7"), trusted, "198.51.100.7"},
		{"walks right to left past trusted hops", "10.0.0.1:443", xff("198.51.100.7, 10.0.0.9"), trusted, "198.51.100.7"},
		{"malformed hop falls back to peer", "10.0.0.1:443", xff("not-an-ip"), trusted, "10.0.0.1"},
		{"oversized hop falls back to peer", "10.0.0.1:443", xff(strings.Repeat("a", 70000)), trusted, "10.0.0.1"},
		{"spoofed garbage before real client still rejected", "10.0.0.1:443", xff("junk, 198.51.100.7, 10.0.0.9"), trusted, "198.51.100.7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := clientIP(tc.addr, tc.header, tc.trusted); got != tc.want {
				t.Errorf("clientIP(%q, %v) = %q, want %q", tc.addr, tc.header.Values("X-Forwarded-For"), got, tc.want)
			}
		})
	}
}

func TestBoundKeyCapsLength(t *testing.T) {
	t.Parallel()
	// A short key passes through unchanged (human-readable, no allocation).
	if got := boundKey("ip:198.51.100.7"); got != "ip:198.51.100.7" {
		t.Errorf("boundKey short = %q, want unchanged", got)
	}
	// An oversized key (only reachable via an attacker-influenced dimension) is
	// collapsed to a fixed-length digest so the bucket map can't hold large strings.
	big := "ip:" + strings.Repeat("a", 100_000)
	got := boundKey(big)
	if len(got) > maxRateLimitKeyBytes {
		t.Errorf("boundKey did not cap: len=%d > %d", len(got), maxRateLimitKeyBytes)
	}
	if !strings.HasPrefix(got, "h:") {
		t.Errorf("bounded key = %q, want a hashed 'h:' key", got)
	}
}
