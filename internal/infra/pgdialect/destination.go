package pgdialect

import (
	"context"
	"errors"
	"net"
	"net/netip"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aportcullis/portcullis/internal/domain/connection"
)

// errDestinationRefused marks a dial the destination policy refused; classify reports it as one bucket whatever rule or address refused it (ADR-0051).
var errDestinationRefused = errors.New("pgdialect: destination refused by policy")

// buildGuardedConfig assembles the pinned connection config and confines its resolution and dialing to the destination policy.
func (d *Dialect) buildGuardedConfig(target connection.Target, mode connection.TLSMode, cred connection.Credential) (*pgconn.Config, error) {
	cfg, err := buildConfig(target, mode, cred, d.validateTimeout)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{}
	cfg.LookupFunc = d.lookupPermittedAddresses
	cfg.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		if !d.isPermittedDialAddress(address) {
			return nil, errDestinationRefused
		}
		return dialer.DialContext(ctx, network, address)
	}
	return cfg, nil
}

// lookupPermittedAddresses resolves host once and returns its addresses as literals only when every one is permitted, so a later re-resolution cannot swap in an unchecked address.
func (d *Dialect) lookupPermittedAddresses(ctx context.Context, host string) ([]string, error) {
	addresses, err := d.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, &net.DNSError{Err: "no addresses", Name: host, IsNotFound: true}
	}
	literals := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if !d.destinations.Permits(address) {
			return nil, errDestinationRefused
		}
		literals = append(literals, address.WithZone("").Unmap().String())
	}
	return literals, nil
}

// isPermittedDialAddress re-checks the host:port pgconn dials, including the cancel-request dial, and refuses anything that is not a permitted address literal.
func (d *Dialect) isPermittedDialAddress(hostPort string) bool {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		return false
	}
	address, err := netip.ParseAddr(host)
	return err == nil && d.destinations.Permits(address)
}
