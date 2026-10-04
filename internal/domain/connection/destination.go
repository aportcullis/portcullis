package connection

import (
	"fmt"
	"net/netip"
	"strings"
)

// MaxDestinationPrefixes bounds each operator CIDR list so a configuration mistake cannot turn every dial into a long scan.
const MaxDestinationPrefixes = 256

// alwaysRefusedDestinations are never dialable whatever the operator lists: unspecified, link-local, multicast and broadcast ranges plus cloud instance-metadata endpoints outside them (ADR-0051).
var alwaysRefusedDestinations = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("ff00::/8"),
	netip.MustParsePrefix("255.255.255.255/32"),
	netip.MustParsePrefix("fd00:ec2::254/128"),
	netip.MustParsePrefix("100.100.100.200/32"),
}

// DestinationPolicy decides which resolved network addresses a connection test or governed execution may dial (ADR-0051). The zero value is the default policy: loopback is refused unless an allow entry names it.
type DestinationPolicy struct {
	allowed []netip.Prefix
	denied  []netip.Prefix
}

// NewDestinationPolicy parses the operator allow and deny CIDR lists; blank entries are ignored and every failure returns ErrInvalidDestinationPolicy.
func NewDestinationPolicy(allowedCIDRs, deniedCIDRs []string) (DestinationPolicy, error) {
	allowed, err := parseDestinationPrefixes(allowedCIDRs)
	if err != nil {
		return DestinationPolicy{}, err
	}
	denied, err := parseDestinationPrefixes(deniedCIDRs)
	if err != nil {
		return DestinationPolicy{}, err
	}
	return DestinationPolicy{allowed: allowed, denied: denied}, nil
}

// Permits reports whether a dial to address is allowed: always-refused ranges, then operator denials, then the allow list (or, without one, everything except loopback).
func (p DestinationPolicy) Permits(address netip.Addr) bool {
	if !address.IsValid() {
		return false
	}
	// Prefixes never contain zoned or IPv4-mapped addresses, so both spellings are canonicalized before matching.
	address = address.WithZone("").Unmap()
	if containsDestination(alwaysRefusedDestinations, address) || containsDestination(p.denied, address) {
		return false
	}
	if len(p.allowed) > 0 {
		return containsDestination(p.allowed, address)
	}
	return !address.IsLoopback()
}

// parseDestinationPrefixes parses canonical CIDR entries, refusing forms that could never match an unmapped, unzoned address.
func parseDestinationPrefixes(entries []string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return nil, fmt.Errorf("%w: %q is not a CIDR", ErrInvalidDestinationPolicy, entry)
		}
		if prefix != prefix.Masked() {
			return nil, fmt.Errorf("%w: %q has host bits set; use %s", ErrInvalidDestinationPolicy, entry, prefix.Masked())
		}
		if prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("%w: %q is IPv4-mapped; list the IPv4 range instead", ErrInvalidDestinationPolicy, entry)
		}
		prefixes = append(prefixes, prefix)
		if len(prefixes) > MaxDestinationPrefixes {
			return nil, fmt.Errorf("%w: more than %d entries", ErrInvalidDestinationPolicy, MaxDestinationPrefixes)
		}
	}
	return prefixes, nil
}

// containsDestination reports whether any prefix contains address.
func containsDestination(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
