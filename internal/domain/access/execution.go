package access

import (
	"errors"
	"time"
)

// Execution lease timings follow PRD §4.4.
const (
	ExecutionHeartbeatInterval = 15 * time.Second
	ExecutionLeaseDuration     = 60 * time.Second
	ExecutionReconcileInterval = 30 * time.Second
)

// Execution errors refuse replay and stale completion without revealing payloads.
var (
	ErrNotExecutable     = errors.New("access: request is not executable")
	ErrLeaseLost         = errors.New("access: execution lease lost")
	ErrPayloadIntegrity  = errors.New("access: approval payload integrity check failed")
	ErrTargetUnavailable = errors.New("access: target temporarily unavailable")
)

// ExecutionLease identifies the single execution attempt and its owning server.
type ExecutionLease struct {
	RequestID RequestID
	Owner     string
	AttemptID string
	Deadline  time.Time
	Heartbeat time.Time
}

// ExecutionCompletion records confirmed execution metadata without result values.
type ExecutionCompletion struct {
	State                State
	RowsAffected         int64
	DurationMilliseconds int64
	ResultID             string
	ResultExpiresAt      *time.Time
	RowCount             int64
	ByteCount            int64
	Truncated            bool
}

// Valid reports whether this completion can terminate an executing request.
func (c ExecutionCompletion) Valid() bool {
	return c.State == StateSucceeded || c.State == StateFailed || c.State == StateCancelled || c.State == StateOutcomeUnknown
}
