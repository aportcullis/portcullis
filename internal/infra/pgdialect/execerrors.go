package pgdialect

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// errExecFailed is the redacted stand-in for any driver error that is neither a PG error nor a context end: its text must never surface (PRD §8.1).
var errExecFailed = errors.New("pgdialect: execution failed")

// redactExecError prioritizes context cancellation for outcome_unknown and drops row-bearing error fields (ADR-0016). The primary message is requester-only; Error() and audit retain SQLSTATE.
func redactExecError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("execution interrupted: %w", ctxErr)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("execution interrupted: %w", err)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return &query.ExecError{SQLState: pgErr.Code, Message: pgErr.Message, Position: int(pgErr.Position)}
	}
	return errExecFailed
}
