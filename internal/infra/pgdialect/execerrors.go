package pgdialect

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// errExecFailed is the redacted stand-in for any driver error that is
// neither a PG error nor a context end: its text must never surface
// (PRD §8.1).
var errExecFailed = errors.New("pgdialect: execution failed")

// redactExecError maps an execution-phase failure onto the caller-safe
// vocabulary (ADR-0016). A canceled/expired context wins over whatever the
// driver observed — including the server-side 57014 our own cancel request
// provoked — so the caller can record outcome_unknown (PRD §8.2). PG errors
// keep SQLSTATE + primary message + position only; Detail/Hint/Where can
// embed row data and are dropped. The primary message can itself quote input
// values, so ExecError carries it as a requester-facing field and keeps it
// out of Error() (the string a log line would capture).
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
