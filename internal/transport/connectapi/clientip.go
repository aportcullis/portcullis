package connectapi

import (
	"context"
	"net"
	"net/http"
	"strings"

	"connectrpc.com/connect"

	"github.com/aportcullis/portcullis/internal/platform/reqmeta"
)

// NewClientIPInterceptor runs first and shares one trusted-proxy-aware client IP with rate limiting and audit.
func NewClientIPInterceptor(trustedProxies []*net.IPNet) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			ip := canonicalIP(clientIP(req.Peer().Addr, req.Header(), trustedProxies))
			return next(reqmeta.WithClientIP(ctx, ip), req)
		}
	}
}

// canonicalIP gives equivalent IP spellings one rate-limit key and audit identity; unparsable values remain unchanged.
func canonicalIP(ip string) string {
	if p := net.ParseIP(ip); p != nil {
		return p.String()
	}
	return ip
}

// clientIP walks trusted X-Forwarded-For hops right-to-left to exclude client-prepended spoofed entries. Untrusted peers use their direct IP.
func clientIP(addr string, h http.Header, trusted []*net.IPNet) string {
	// No trusted proxies (the common direct-exposure case): the peer is the client; skip parsing the peer IP and the X-Forwarded-For machinery entirely.
	if len(trusted) == 0 {
		return hostOnly(addr)
	}
	peer := hostOnly(addr)
	if !ipInNets(peer, trusted) {
		return peer
	}
	forwarded := parseForwardedFor(h)
	for idx := len(forwarded) - 1; idx >= 0; idx-- {
		// Stop trusting a malformed forwarded hop and use the peer IP so proxy input cannot forge audit identities or unbounded rate-limit keys.
		if net.ParseIP(forwarded[idx]) == nil {
			return peer
		}
		if !ipInNets(forwarded[idx], trusted) {
			return forwarded[idx]
		}
	}
	return peer
}

// hostOnly strips the port from a "host:port" address, falling back to the raw value (some transports report a bare host).
func hostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// parseForwardedFor returns the X-Forwarded-For addresses in order (leftmost = original client, rightmost = nearest proxy).
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
