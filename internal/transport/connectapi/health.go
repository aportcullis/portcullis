// Package connectapi holds the Connect RPC service implementations
// (presentation layer).
package connectapi

import (
	"context"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
)

// HealthService implements the Health RPC. It validates the end-to-end RPC
// pipeline and reports basic liveness; dependency readiness is served separately
// by the /readyz probe.
type HealthService struct{}

// Check returns a static ok status.
func (HealthService) Check(
	_ context.Context,
	_ *connect.Request[portcullisv1.CheckRequest],
) (*connect.Response[portcullisv1.CheckResponse], error) {
	return connect.NewResponse(&portcullisv1.CheckResponse{Status: "ok"}), nil
}
