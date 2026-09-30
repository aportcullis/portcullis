package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// readSnapshot keeps multi-statement page reads, totals, and clamps in one read-only snapshot.
func readSnapshot(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, fn func(pgx.Tx, *db.Queries) error) error {
	// Repeatable Read takes the snapshot once, at the first statement, and every later statement in the transaction reuses it. It costs nothing here: only updating transactions can hit a serialization failure, and PostgreSQL is explicit that "read-only transactions will never have serialization conflicts" — so there is no retry path to build.
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only: nothing to lose on rollback
	if err := fn(tx, q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
