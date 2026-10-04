package pgdialect

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// errExecFailed is the redacted stand-in for any driver error that is neither a PG error nor a context end: its text must never surface (PRD §8.1).
var errExecFailed = errors.New("pgdialect: execution failed")

// redactExecError redacts a failure observed before COMMIT was sent: a context end is marked query.ErrInterruptedBeforeCommit because the transaction cannot have committed, and row-bearing error fields are dropped (ADR-0016, ADR-0021).
func redactExecError(ctx context.Context, err error) error {
	if cause := interruptionCause(ctx, err); cause != nil {
		return fmt.Errorf("execution interrupted: %w: %w", query.ErrInterruptedBeforeCommit, cause)
	}
	return redactServerError(err)
}

// redactCommitError redacts a COMMIT failure: a context end while COMMIT was in flight leaves the outcome unconfirmed, so it carries no rollback marker.
func redactCommitError(ctx context.Context, err error) error {
	if cause := interruptionCause(ctx, err); cause != nil {
		return fmt.Errorf("execution interrupted: %w", cause)
	}
	return redactServerError(err)
}

// interruptionCause returns the context end that interrupted err, preferring the execution context's own state.
func interruptionCause(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

// redactServerError keeps only SQLSTATE, the requester-only message and the position of a server error. The primary message is requester-only; Error() and audit retain SQLSTATE.
func redactServerError(err error) error {
	var responseLimit *pgproto3.ExceededMaxBodyLenErr
	if errors.As(err, &responseLimit) {
		return query.ErrResponseLimit
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return &query.ExecError{SQLState: pgErr.Code, Message: pgErr.Message, Position: int(pgErr.Position)}
	}
	return errExecFailed
}
