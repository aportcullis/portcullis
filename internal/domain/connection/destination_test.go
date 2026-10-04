package connection_test

import (
	"errors"
	"net/netip"
	"strconv"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/connection"
)

func TestDestinationPolicyPermitsAddresses(t *testing.T) {
	t.Parallel()
	loopbackAllowed := mustDestinationPolicy(t, []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8"}, nil)
	everythingAllowed := mustDestinationPolicy(t, []string{"0.0.0.0/0", "::/0"}, nil)
	narrowedByDeny := mustDestinationPolicy(t, []string{"10.0.0.0/8"}, []string{"10.0.5.0/24"})
	privateDenied := mustDestinationPolicy(t, nil, []string{"10.0.0.0/8", "fc00::/7"})

	tests := []struct {
		name    string
		policy  connection.DestinationPolicy
		address string
		want    bool
	}{
		{"default permits a private IPv4 database", connection.DestinationPolicy{}, "10.20.30.40", true},
		{"default permits a public IPv6 database", connection.DestinationPolicy{}, "2001:db8::10", true},
		{"default permits an IPv4-mapped private address", connection.DestinationPolicy{}, "::ffff:192.168.1.7", true},
		{"explicit allow permits loopback", loopbackAllowed, "127.0.0.1", true},
		{"explicit allow permits IPv6 loopback", loopbackAllowed, "::1", true},
		{"allow list permits a member outside the deny carve-out", narrowedByDeny, "10.0.6.1", true},
		{"deny list leaves unlisted ranges permitted", privateDenied, "192.168.1.1", true},

		{"default refuses IPv4 loopback", connection.DestinationPolicy{}, "127.0.0.1", false},
		{"default refuses IPv6 loopback", connection.DestinationPolicy{}, "::1", false},
		{"default refuses IPv4-mapped loopback", connection.DestinationPolicy{}, "::ffff:127.0.0.1", false},
		{"default refuses zoned IPv6 loopback", connection.DestinationPolicy{}, "::1%lo0", false},
		{"cloud metadata is refused even when everything is allowed", everythingAllowed, "169.254.169.254", false},
		{"IPv4-mapped cloud metadata is refused", everythingAllowed, "::ffff:169.254.169.254", false},
		{"AWS IPv6 metadata endpoint is refused", everythingAllowed, "fd00:ec2::254", false},
		{"Alibaba metadata endpoint is refused", everythingAllowed, "100.100.100.200", false},
		{"IPv6 link-local is refused", everythingAllowed, "fe80::1", false},
		{"zoned IPv6 link-local is refused", everythingAllowed, "fe80::1%en0", false},
		{"IPv4 unspecified is refused", everythingAllowed, "0.0.0.0", false},
		{"IPv6 unspecified is refused", everythingAllowed, "::", false},
		{"IPv4 multicast is refused", everythingAllowed, "224.0.0.1", false},
		{"IPv6 multicast is refused", everythingAllowed, "ff02::1", false},
		{"limited broadcast is refused", everythingAllowed, "255.255.255.255", false},
		{"deny carve-out wins over allow", narrowedByDeny, "10.0.5.9", false},
		{"allow list refuses unlisted public address", loopbackAllowed, "8.8.8.8", false},
		{"deny list refuses an IPv4-mapped spelling of a denied address", privateDenied, "::ffff:10.1.1.1", false},
		{"deny list refuses unique-local IPv6", privateDenied, "fd12:3456::1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			address := netip.MustParseAddr(tt.address)
			if got := tt.policy.Permits(address); got != tt.want {
				t.Fatalf("Permits(%s) = %v, want %v", tt.address, got, tt.want)
			}
		})
	}
}

func TestDestinationPolicyRefusesTheInvalidAddress(t *testing.T) {
	t.Parallel()
	everythingAllowed := mustDestinationPolicy(t, []string{"0.0.0.0/0", "::/0"}, nil)
	if everythingAllowed.Permits(netip.Addr{}) {
		t.Fatal("the zero address must never be dialable")
	}
}

func TestNewDestinationPolicyValidatesCIDRLists(t *testing.T) {
	t.Parallel()
	tooMany := make([]string, connection.MaxDestinationPrefixes+1)
	for idx := range tooMany {
		tooMany[idx] = "10." + strconv.Itoa(idx/256) + "." + strconv.Itoa(idx%256) + ".0/24"
	}
	atLimit := tooMany[:connection.MaxDestinationPrefixes]

	tests := []struct {
		name    string
		allowed []string
		denied  []string
		wantErr bool
	}{
		{"empty lists", nil, nil, false},
		{"blank entries are ignored", []string{" ", ""}, []string{""}, false},
		{"surrounding whitespace is trimmed", []string{" 10.0.0.0/8 "}, []string{" fd00::/8"}, false},
		{"exactly the bound", atLimit, atLimit, false},

		{"not a CIDR", []string{"10.0.0.0"}, nil, true},
		{"garbage", nil, []string{"internal-network"}, true},
		{"host bits set", []string{"10.0.0.1/8"}, nil, true},
		{"IPv4-mapped prefix never matches unmapped addresses", nil, []string{"::ffff:10.0.0.0/104"}, true},
		{"zoned prefix", []string{"fe80::%en0/64"}, nil, true},
		{"allow list over the bound", tooMany, nil, true},
		{"deny list over the bound", nil, tooMany, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := connection.NewDestinationPolicy(tt.allowed, tt.denied)
			if tt.wantErr != errors.Is(err, connection.ErrInvalidDestinationPolicy) {
				t.Fatalf("NewDestinationPolicy err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("NewDestinationPolicy err = %v", err)
			}
		})
	}
}

func mustDestinationPolicy(t *testing.T, allowed, denied []string) connection.DestinationPolicy {
	t.Helper()
	policy, err := connection.NewDestinationPolicy(allowed, denied)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}
