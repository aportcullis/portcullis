package pgdialect_test

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
)

// fixedResolver answers lookups from a fixed table so a hostname can resolve to any address, including ones an attacker controls DNS for.
type fixedResolver map[string][]netip.Addr

func (resolver fixedResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if addresses, ok := resolver[host]; ok {
		return addresses, nil
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{address}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// countingListener accepts TCP connections on IPv4 loopback and counts them, so a refused destination can be shown never to have been dialed.
type countingListener struct {
	port     int
	accepted atomic.Int64
}

func listenCounting(t *testing.T) *countingListener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	counter := &countingListener{port: port}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			counter.accepted.Add(1)
			_ = conn.Close()
		}
	}()
	return counter
}

func resolveTargetAddresses(t *testing.T, host string) []netip.Addr {
	t.Helper()
	addresses, err := net.DefaultResolver.LookupNetIP(context.Background(), "ip", host)
	if err != nil || len(addresses) == 0 {
		t.Fatalf("resolve Testcontainers host %q: %v", host, err)
	}
	return addresses
}

func exactDestinationCIDRs(addresses []netip.Addr) []string {
	cidrs := make([]string, 0, len(addresses))
	for _, address := range addresses {
		address = address.WithZone("").Unmap()
		cidrs = append(cidrs, netip.PrefixFrom(address, address.BitLen()).String())
	}
	return cidrs
}

func mustDestinationPolicy(t *testing.T, allowed, denied []string) connection.DestinationPolicy {
	t.Helper()
	policy, err := connection.NewDestinationPolicy(allowed, denied)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func mustTarget(t *testing.T, host string, port int) connection.Target {
	t.Helper()
	target, err := connection.NewTarget(host, port, "portcullis")
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func executeGovernedSelect(ctx context.Context, dialect *pgdialect.Dialect, target connection.Target, cred connection.Credential) error {
	stream, err := dialect.Execute(ctx, target, connection.TLSModeDisable, cred, query.Execution{SQL: "SELECT 1", Class: query.ClassRead, MaxRows: 10, MaxResultBytes: 4096, TimeoutSeconds: 30})
	if stream != nil {
		_ = stream.Close()
	}
	return err
}

func TestDestinationPolicyPermitsCheckedTargets(t *testing.T) {
	t.Parallel()
	target, cred := pgCoords(t)
	addresses := resolveTargetAddresses(t, target.Host)
	var ipv4Address netip.Addr
	for _, address := range addresses {
		if address.Unmap().Is4() {
			ipv4Address = address.Unmap()
		}
	}

	cases := []struct {
		name     string
		host     string
		policy   connection.DestinationPolicy
		resolver pgdialect.DestinationResolver
		needIPv4 bool
	}{
		{"published host under the target ranges", target.Host, dbtest.TargetDestinationPolicy(t), nil, false},
		{"unregistered hostname dialed through its checked resolution", "warehouse.portcullis.test", dbtest.TargetDestinationPolicy(t), fixedResolver{"warehouse.portcullis.test": addresses}, false},
		{"allow list naming only the resolved addresses", target.Host, mustDestinationPolicy(t, exactDestinationCIDRs(addresses), nil), nil, false},
		{"IPv4-mapped resolution matched as its IPv4 address", "mapped.portcullis.test", mustDestinationPolicy(t, exactDestinationCIDRs([]netip.Addr{ipv4Address}), nil), fixedResolver{"mapped.portcullis.test": {netip.AddrFrom16(ipv4Address.As16())}}, true},
		{"deny list elsewhere leaves the target dialable", target.Host, mustDestinationPolicy(t, dbtest.TargetDestinationCIDRs, []string{"10.255.0.0/16", "fd00:dead::/32"}), nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.needIPv4 && !ipv4Address.IsValid() {
				t.Skip("Testcontainers host has no IPv4 address")
			}
			dialect := pgdialect.New(pgdialect.Options{ValidateTimeout: 10 * time.Second, Destinations: tc.policy, Resolver: tc.resolver})
			checked := mustTarget(t, tc.host, int(target.Port))
			if err := dialect.ValidateConnection(context.Background(), checked, connection.TLSModeDisable, cred); err != nil {
				t.Fatalf("ValidateConnection: %v", err)
			}
			if err := executeGovernedSelect(context.Background(), dialect, checked, cred); err != nil {
				t.Fatalf("Execute: %v", err)
			}
		})
	}
}

func TestDestinationPolicyRefusesUncheckedTargetsWithoutDialing(t *testing.T) {
	t.Parallel()
	listener := listenCounting(t)
	everythingAllowed := mustDestinationPolicy(t, []string{"0.0.0.0/0", "::/0"}, nil)
	loopbackAllowed := mustDestinationPolicy(t, []string{"127.0.0.0/8"}, nil)
	metadata := netip.MustParseAddr("169.254.169.254")
	loopback := netip.MustParseAddr("127.0.0.1")
	attackerDNS := fixedResolver{
		"localhost":               {loopback, netip.IPv6Loopback()},
		"metadata.attacker.test":  {metadata},
		"rebind.attacker.test":    {loopback, metadata},
		"mapped.attacker.test":    {netip.MustParseAddr("::ffff:127.0.0.1")},
		"linklocal.attacker.test": {netip.MustParseAddr("fe80::1%lo0")},
	}

	cases := []struct {
		name   string
		host   string
		policy connection.DestinationPolicy
	}{
		{"loopback literal under the default policy", "127.0.0.1", connection.DestinationPolicy{}},
		{"IPv6 loopback literal under the default policy", "::1", connection.DestinationPolicy{}},
		{"IPv4-mapped loopback literal under the default policy", "::ffff:127.0.0.1", connection.DestinationPolicy{}},
		{"hostname resolving to IPv4-mapped loopback", "mapped.attacker.test", connection.DestinationPolicy{}},
		{"hostname resolving to cloud metadata even when everything is allowed", "metadata.attacker.test", everythingAllowed},
		{"hostname resolving to an allowed and a refused address", "rebind.attacker.test", loopbackAllowed},
		{"hostname resolving to zoned IPv6 link-local", "linklocal.attacker.test", everythingAllowed},
		{"IPv4-mapped metadata literal", "::ffff:169.254.169.254", everythingAllowed},
		{"operator deny carve-out inside the allow list", "127.0.0.1", mustDestinationPolicy(t, []string{"127.0.0.0/8"}, []string{"127.0.0.1/32"})},
		{"loopback outside a private-only allow list", "localhost", mustDestinationPolicy(t, []string{"10.0.0.0/8"}, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dialect := pgdialect.New(pgdialect.Options{ValidateTimeout: 5 * time.Second, Destinations: tc.policy, Resolver: attackerDNS})
			refused := mustTarget(t, tc.host, listener.port)
			cred, err := connection.NewCredential("prober", "pw-destination-refused")
			if err != nil {
				t.Fatal(err)
			}
			before := listener.accepted.Load()
			assertBucket(t, dialect.ValidateConnection(context.Background(), refused, connection.TLSModeDisable, cred), connection.TestBucketDestinationRefused, cred.Password)
			assertBucket(t, executeGovernedSelect(context.Background(), dialect, refused, cred), connection.TestBucketDestinationRefused, cred.Password)
			if accepted := listener.accepted.Load() - before; accepted != 0 {
				t.Fatalf("refused destination was dialed %d times", accepted)
			}
		})
	}
}
