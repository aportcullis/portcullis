// Package postgres is the metadata-store infrastructure: connection pool and schema migrations. It implements (or backs) the domain repository ports.
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open creates a connection pool to the metadata database.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, dsn)
}
