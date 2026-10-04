package access

import (
	"errors"
	"time"
)

// Execution lease timings follow PRD §4.4; a reconciliation run is bounded by its timeout and recovers overdue attempts in batches.
const (
	ExecutionHeartbeatInterval  = 15 * time.Second
	ExecutionLeaseDuration      = 60 * time.Second
	ExecutionReconcileInterval  = 30 * time.Second
	ExecutionReconcileTimeout   = 10 * time.Second
	ExecutionReconcileBatchSize = 100
)

// ReconcileCursor orders overdue attempts by deadline then request; the zero cursor starts before the earliest.
type ReconcileCursor struct {
	Deadline  time.Time
	RequestID RequestID
}

// IsStart reports whether the cursor starts before every overdue attempt.
func (c ReconcileCursor) IsStart() bool { return c.Deadline.IsZero() && c.RequestID == "" }

// ReconcileBatch reports one reconciliation batch: listed attempts, those recorded outcome_unknown, those locked by another transaction, and those whose recovery failed.
type ReconcileBatch struct {
	Listed    int
	Recovered int
	Skipped   int
	Failed    int
	// Next resumes listing after the last listed attempt, so a failing attempt never blocks later ones.
	Next ReconcileCursor
	// FirstFailure is one failed attempt's cause, for classified logging only.
	FirstFailure error
}

// ReconcileSummary totals the batches of one reconciliation run.
type ReconcileSummary struct {
	Recovered    int
	Skipped      int
	Failed       int
	FirstFailure error
}

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

// ResultUnavailableReason names why a confirmed success has no stored result, without carrying store or driver error text.
type ResultUnavailableReason string

// Result unavailability reasons recorded as execution evidence (ADR-0011, ADR-0021).
const (
	ResultUnavailableStoreFull         ResultUnavailableReason = "result_store_full"
	ResultUnavailablePersistenceFailed ResultUnavailableReason = "result_persistence_failed"
)

// InterruptionCause names who interrupted an execution before it completed, recorded as audit evidence.
type InterruptionCause string

// Interruption causes recorded on EXECUTION_FINISHED (ADR-0021).
const (
	InterruptedByOwner     InterruptionCause = "owner_cancel"
	InterruptedByShutdown  InterruptionCause = "server_shutdown"
	InterruptedByLeaseLoss InterruptionCause = "lease_lost"
)

// ExecutionCompletion records confirmed execution metadata without result values.
type ExecutionCompletion struct {
	State                   State
	RowsAffected            int64
	DurationMilliseconds    int64
	ResultID                string
	ResultExpiresAt         *time.Time
	RowCount                int64
	ByteCount               int64
	Truncated               bool
	ResultUnavailableReason ResultUnavailableReason
	InterruptionCause       InterruptionCause
}

// Valid reports whether this completion can terminate an executing request; a result unavailability reason is allowed only on a success without a stored result, and an interruption cause only on an execution that did not succeed.
func (c ExecutionCompletion) Valid() bool {
	if c.State != StateSucceeded && c.State != StateFailed && c.State != StateCancelled && c.State != StateOutcomeUnknown {
		return false
	}
	switch c.InterruptionCause {
	case "":
	case InterruptedByOwner, InterruptedByShutdown, InterruptedByLeaseLoss:
		if c.State == StateSucceeded {
			return false
		}
	default:
		return false
	}
	switch c.ResultUnavailableReason {
	case "":
		return true
	case ResultUnavailableStoreFull, ResultUnavailablePersistenceFailed:
		return c.State == StateSucceeded && c.ResultID == ""
	}
	return false
}
