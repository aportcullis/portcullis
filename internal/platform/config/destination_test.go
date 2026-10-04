package config_test

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/platform/config"
)

func TestConnectionDestinationPolicyLoadsOperatorLists(t *testing.T) {
	cases := []struct {
		name      string
		allowed   string
		denied    string
		permitted []string
		refused   []string
	}{
		{"default refuses loopback and permits private targets", "", "", []string{"10.1.2.3", "192.168.0.10", "2001:db8::5"}, []string{"127.0.0.1", "::1", "169.254.169.254"}},
		{"explicit loopback allow for a co-located database", "127.0.0.0/8, ::1/128", "", []string{"127.0.0.1", "::1"}, []string{"10.1.2.3", "fe80::1"}},
		{"deny carve-out inside a private allow list", "10.0.0.0/8", "10.9.0.0/16", []string{"10.1.2.3"}, []string{"10.9.0.1", "192.168.0.10"}},
		{"deny list alone narrows the default", "", "192.168.0.0/16,fc00::/7", []string{"10.1.2.3"}, []string{"192.168.1.1", "fd00::1", "127.0.0.1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PORTCULLIS_CONNECTION_ALLOWED_CIDRS", tc.allowed)
			t.Setenv("PORTCULLIS_CONNECTION_DENIED_CIDRS", tc.denied)
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			policy := cfg.ConnectionDestinationPolicy()
			for _, address := range tc.permitted {
				if !policy.Permits(netip.MustParseAddr(address)) {
					t.Errorf("%s refused, want permitted", address)
				}
			}
			for _, address := range tc.refused {
				if policy.Permits(netip.MustParseAddr(address)) {
					t.Errorf("%s permitted, want refused", address)
				}
			}
		})
	}
}

func TestConnectionDestinationPolicyRejectsInvalidLists(t *testing.T) {
	tooMany := strings.TrimSuffix(strings.Repeat("10.0.0.0/8,", 257), ",")
	cases := []struct {
		name    string
		env     string
		value   string
		wantKey string
	}{
		{"allow entry without a prefix length", "PORTCULLIS_CONNECTION_ALLOWED_CIDRS", "10.0.0.5", "connection_allowed_cidrs"},
		{"deny entry with host bits", "PORTCULLIS_CONNECTION_DENIED_CIDRS", "192.168.1.1/16", "connection_denied_cidrs"},
		{"IPv4-mapped allow entry", "PORTCULLIS_CONNECTION_ALLOWED_CIDRS", "::ffff:127.0.0.0/104", "connection_allowed_cidrs"},
		{"hostname instead of a CIDR", "PORTCULLIS_CONNECTION_DENIED_CIDRS", "metadata.google.internal", "connection_denied_cidrs"},
		{"allow list over the bound", "PORTCULLIS_CONNECTION_ALLOWED_CIDRS", tooMany, "connection_allowed_cidrs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.env, tc.value)
			_, err := config.Load()
			if err == nil {
				t.Fatalf("Load accepted the %s value", tc.env)
			}
			if !strings.Contains(err.Error(), tc.wantKey) {
				t.Errorf("error %q does not name %s", err, tc.wantKey)
			}
		})
	}
}
