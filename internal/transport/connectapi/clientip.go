package connectapi

import (
	"context"
	"net"
	"net/http"
	"strings"

	"connectrpc.com/connect"

	"github.com/aportcullis/portcullis/internal/platform/reqmeta"
)

// NewClientIPInterceptor resolves the real client IP once per request and stashes
// it in the context (reqmeta.ClientIP), so the rate limiter and the audit trail
// share one source of truth instead of each re-deriving it. It runs first in the
// chain, for every procedure. trustedProxies are the networks whose
// X-Forwarded-For is honored to look past the proxy to the originating client.
func NewClientIPInterceptor(trustedProxies []*net.IPNet) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			ip := canonicalIP(clientIP(req.Peer().Addr, req.Header(), trustedProxies))
			return next(reqmeta.WithClientIP(ctx, ip), req)
		}
	}
}

// canonicalIP normalizes textual IP forms (e.g. IPv6 zero-compression, so
// "2001:0db8::1" and "2001:db8::1" read as one client). It runs here, where the
// value is minted, so EVERY consumer — the rate limiter's bucket key and the
// audit trail's source_ip — sees the same form and stays correlatable
// (ADR-0010). An unparsable value stays as-is: it is its own key, same as
// clientIP's untrusted fallback.
func canonicalIP(ip string) string {
	if p := net.ParseIP(ip); p != nil {
		return p.String()
	}
	return ip
}

// clientIP resolves the address to attribute a request to. When the direct peer
// is a trusted proxy, it walks X-Forwarded-For right-to-left (nearest hop first)
// and returns the first address that is NOT itself a trusted proxy — the closest
// client the trusted chain vouches for. Walking from the right is essential: a
// client can prepend spoofed entries on the left, but everything from the real
// connection rightward is written by trusted proxies. Untrusted peers (or a
// missing header) fall back to the peer IP, so a client can't spoof its key.
func clientIP(addr string, h http.Header, trusted []*net.IPNet) string {
	// No trusted proxies (the common direct-exposure case): the peer is the client;
	// skip parsing the peer IP and the X-Forwarded-For machinery entirely.
	if len(trusted) == 0 {
		return hostOnly(addr)
	}
	peer := hostOnly(addr)
	if !ipInNets(peer, trusted) {
		return peer
	}
	forwarded := parseForwardedFor(h)
	for i := len(forwarded) - 1; i >= 0; i-- {
		// A hop that is not a valid IP can't be a real client address: a trusted
		// proxy could forward a malformed/oversized value, or an attacker prepends
		// one past the known-proxy hops. Stop trusting the chain here and attribute
		// to the peer, so a spoofed X-Forwarded-For can never mint an arbitrary
		// (unbounded) rate-limit key or a bogus audit source_ip (ADR-0010).
		if net.ParseIP(forwarded[i]) == nil {
			return peer
		}
		if !ipInNets(forwarded[i], trusted) {
			return forwarded[i]
		}
	}
	return peer
}

// hostOnly strips the port from a "host:port" address, falling back to the raw
// value (some transports report a bare host).
func hostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// parseForwardedFor returns the X-Forwarded-For addresses in order (leftmost =
// original client, rightmost = nearest proxy).
func parseForwardedFor(h http.Header) []string {
	var out []string
	for _, line := range h.Values("X-Forwarded-For") {
		for _, part := range strings.Split(line, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// ipInNets reports whether ip parses and falls inside any of the networks.
func ipInNets(ip string, nets []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}
