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
			ip := clientIP(req.Peer().Addr, req.Header(), trustedProxies)
			return next(reqmeta.WithClientIP(ctx, ip), req)
		}
	}
}

// clientIP resolves the address to attribute a request to. When the direct peer
// is a trusted proxy, it walks X-Forwarded-For right-to-left (nearest hop first)
// and returns the first address that is NOT itself a trusted proxy — the closest
// client the trusted chain vouches for. Walking from the right is essential: a
// client can prepend spoofed entries on the left, but everything from the real
// connection rightward is written by trusted proxies. Untrusted peers (or a
// missing header) fall back to the peer IP, so a client can't spoof its key.
func clientIP(addr string, h http.Header, trusted []*net.IPNet) string {
	peer := hostOnly(addr)
	if !ipInNets(peer, trusted) {
		return peer
	}
	forwarded := parseForwardedFor(h)
	for i := len(forwarded) - 1; i >= 0; i-- {
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
