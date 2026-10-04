// Package publicorigin parses the operator-configured public origins and answers which Host headers and browser origins belong to this installation (ADR-0052).
package publicorigin

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ParsePolicy validates and normalizes absolute origins; an empty list selects the loopback-only default.
func ParsePolicy(origins []string) (Policy, error) {
	policy := Policy{authorities: map[string]bool{}}
	for _, raw := range origins {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		origin, authorities, err := normalizeOrigin(trimmed)
		if err != nil {
			return Policy{}, err
		}
		policy.origins = append(policy.origins, origin)
		for _, authority := range authorities {
			policy.authorities[authority] = true
		}
	}
	return policy, nil
}

// normalizeOrigin returns the canonical scheme://host[:port] form and every Host header value that addresses it. Errors omit the input because an operator may paste credentials into a URL.
func normalizeOrigin(raw string) (string, []string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", nil, fmt.Errorf("invalid public origin: not a URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", nil, fmt.Errorf("invalid public origin: scheme must be http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.Opaque != "" {
		return "", nil, fmt.Errorf("invalid public origin: only scheme, host and optional port are allowed")
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" || strings.Contains(hostname, "*") {
		return "", nil, fmt.Errorf("invalid public origin: an exact host name is required")
	}
	defaultPort := defaultHTTPSPort
	if parsed.Scheme == "http" {
		defaultPort = defaultHTTPPort
	}
	port := parsed.Port()
	if port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > maxPortNumber {
			return "", nil, fmt.Errorf("invalid public origin: port must be between 1 and %d", maxPortNumber)
		}
	}
	hostLiteral := hostname
	if strings.Contains(hostname, ":") {
		hostLiteral = "[" + hostname + "]"
	}
	if port == "" || port == defaultPort {
		return parsed.Scheme + "://" + hostLiteral, []string{hostLiteral, hostLiteral + ":" + defaultPort}, nil
	}
	authority := hostLiteral + ":" + port
	return parsed.Scheme + "://" + authority, []string{authority}, nil
}

// AllowsHost reports whether a request Host header names this installation.
func (p Policy) AllowsHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if !p.IsLoopbackDefault() {
		return p.authorities[host]
	}
	return isLoopbackHostname(hostnameWithoutPort(host))
}

// hostnameWithoutPort strips an optional port and IPv6 brackets from a Host header value.
func hostnameWithoutPort(host string) string {
	if hostname, _, err := net.SplitHostPort(host); err == nil {
		return hostname
	}
	return strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
}

// isLoopbackHostname reports whether a hostname can only address the local machine, which a rebinding attacker's page cannot present as its Host.
func isLoopbackHostname(hostname string) bool {
	if hostname == loopbackHostname {
		return true
	}
	address := net.ParseIP(hostname)
	return address != nil && address.IsLoopback()
}

// TrustedOrigins returns the configured origins in normalized form; the loopback default returns none.
func (p Policy) TrustedOrigins() []string {
	return append([]string(nil), p.origins...)
}

// IsLoopbackDefault reports whether no public origin is configured and only loopback hosts are admitted.
func (p Policy) IsLoopbackDefault() bool {
	return len(p.origins) == 0
}
