// Package dbtest provides shared, container-backed databases for integration
// tests. Each engine starts once per test binary and is reused; the
// testcontainers reaper tears the containers down when the process exits.
//
// It is the harness for the cross-engine contract suite: PostgreSQL and MySQL
// run in containers, SQLite is file-based (added with the dialect adapters).
package dbtest

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/aportcullis/portcullis/internal/platform/logging"
)

var (
	pgOnce sync.Once
	pgPool *pgxpool.Pool
	pgErr  error
)

// Postgres returns a connection pool to a shared test Postgres. It uses
// PORTCULLIS_TEST_DATABASE_URL when set, otherwise a throwaway container. When
// neither Docker nor an external DB is available, the calling test is skipped.
func Postgres(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pgOnce.Do(startPostgres)
	if pgErr != nil {
		t.Skipf("postgres unavailable: %v", pgErr)
	}
	return pgPool
}

func startPostgres() {
	ctx := context.Background()

	dsn := os.Getenv("PORTCULLIS_TEST_DATABASE_URL")
	if dsn == "" {
		// Route testcontainers output through our standardized slog stream.
		tcLogger := logging.NewPrintfLogger(logging.New("debug", "json"), slog.LevelDebug, "testcontainers")
		container, err := tcpostgres.Run(ctx, "postgres:18.4-alpine3.24",
			tcpostgres.WithDatabase("portcullis"),
			tcpostgres.WithUsername("portcullis"),
			tcpostgres.WithPassword("portcullis"),
			testcontainers.WithLogger(tcLogger),
			testcontainers.WithWaitStrategy(
				wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second)),
		)
		if err != nil {
			pgErr = err
			return
		}
		if dsn, err = container.ConnectionString(ctx, "sslmode=disable"); err != nil {
			pgErr = err
			return
		}
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		pgErr = err
		return
	}
	pgPool = pool
}
