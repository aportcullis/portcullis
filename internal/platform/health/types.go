package health

import "context"

// CheckFunc reports whether a dependency is ready. A nil error means ready.
type CheckFunc func(ctx context.Context) error

// response is the JSON body for the liveness and readiness probes.
type response struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}
