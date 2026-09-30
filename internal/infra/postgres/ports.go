package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// rowQuerier is the single-row query surface shared by *pgxpool.Pool and *pgxpool.Conn, so the runtime-role checks can run on either the migration connection or the runtime pool.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
