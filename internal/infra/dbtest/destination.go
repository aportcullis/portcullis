package dbtest

import (
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/connection"
)

// TargetDestinationCIDRs are the ranges a Testcontainers target is published on: loopback for a local daemon and private ranges for a remote or nested one.
var TargetDestinationCIDRs = []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"}

// TargetDestinationPolicy permits dialing the Testcontainers target, which the default destination policy refuses on loopback (ADR-0051).
func TargetDestinationPolicy(t testing.TB) connection.DestinationPolicy {
	t.Helper()
	policy, err := connection.NewDestinationPolicy(TargetDestinationCIDRs, nil)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}
