package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/infra/dbtest"
)

// The embedded migration set is fixed and already applied, so only a white-box call can offer applyMigration a contended or slow file to observe its lock-timeout retry and statement bound.
func migrationRetryFixture(t *testing.T) (*pgxpool.Pool, *pgxpool.Conn) {
	t.Helper()
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `create table public.schema_migrations (version text primary key, applied_at timestamptz not null default now(), checksum bytea)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `create table public.contended (id int)`); err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Release)
	return pool, conn
}

// holdTableLock keeps an exclusive lock on public.contended for the given duration.
func holdTableLock(t *testing.T, pool *pgxpool.Pool, hold time.Duration) <-chan error {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `lock table public.contended in access exclusive mode`); err != nil {
		t.Fatal(err)
	}
	released := make(chan error, 1)
	go func() {
		time.Sleep(hold)
		released <- tx.Rollback(ctx)
	}()
	return released
}

func recordedMigrationCount(t *testing.T, pool *pgxpool.Pool, version string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `select count(*) from public.schema_migrations where version = $1 and checksum is not null`, version).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func requireSQLState(t *testing.T, err error, code, attempt string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Errorf("%s = %v, want SQLSTATE %s", attempt, err, code)
	}
}

func TestApplyMigrationRetriesLockTimeoutsAndBoundsStatements(t *testing.T) {
	ctx := context.Background()
	quick := migrationTimeouts{lockTimeout: 100 * time.Millisecond, statementTimeout: 5 * time.Second, attempts: 6, retryBackoff: 150 * time.Millisecond}
	contended := func() migrationFile {
		return migrationFile{version: "test_" + uuid.NewString(), body: `alter table public.contended add column if not exists note text`, checksum: []byte("0123456789abcdef0123456789abcdef")}
	}

	t.Run("an uncontended file applies on the first attempt", func(t *testing.T) {
		pool, conn := migrationRetryFixture(t)
		file := contended()
		if err := applyMigration(ctx, conn, file, quick); err != nil {
			t.Fatalf("applyMigration: %v", err)
		}
		if recordedMigrationCount(t, pool, file.version) != 1 {
			t.Error("applied file was not recorded with its checksum")
		}
	})

	t.Run("a briefly held lock is retried until it clears", func(t *testing.T) {
		pool, conn := migrationRetryFixture(t)
		released := holdTableLock(t, pool, 400*time.Millisecond)
		file := contended()
		if err := applyMigration(ctx, conn, file, quick); err != nil {
			t.Fatalf("applyMigration behind a brief lock: %v", err)
		}
		if err := <-released; err != nil {
			t.Fatal(err)
		}
		if recordedMigrationCount(t, pool, file.version) != 1 {
			t.Error("retried file was not recorded exactly once")
		}
	})

	t.Run("lock settings end with the migration transaction", func(t *testing.T) {
		_, conn := migrationRetryFixture(t)
		if err := applyMigration(ctx, conn, contended(), quick); err != nil {
			t.Fatal(err)
		}
		var lockTimeout, statementTimeout string
		if err := conn.QueryRow(ctx, `select current_setting('lock_timeout'), current_setting('statement_timeout')`).Scan(&lockTimeout, &statementTimeout); err != nil {
			t.Fatal(err)
		}
		if lockTimeout != "0" || statementTimeout != "0" {
			t.Errorf("session kept lock_timeout=%s statement_timeout=%s after the migration, want both reset", lockTimeout, statementTimeout)
		}
	})

	t.Run("a lock held past every attempt fails without recording", func(t *testing.T) {
		pool, conn := migrationRetryFixture(t)
		released := holdTableLock(t, pool, 3*time.Second)
		file := contended()
		started := time.Now()
		err := applyMigration(ctx, conn, file, migrationTimeouts{lockTimeout: 100 * time.Millisecond, statementTimeout: 5 * time.Second, attempts: 2, retryBackoff: 100 * time.Millisecond})
		requireSQLState(t, err, lockNotAvailableCode, "applyMigration behind a long lock")
		if elapsed := time.Since(started); elapsed > 2*time.Second {
			t.Errorf("bounded retries took %s", elapsed)
		}
		if recordedMigrationCount(t, pool, file.version) != 0 {
			t.Error("a failed migration was recorded")
		}
		if err := <-released; err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a slow statement hits the statement timeout and is not retried", func(t *testing.T) {
		pool, conn := migrationRetryFixture(t)
		file := migrationFile{version: "test_slow", body: `select pg_sleep(2)`, checksum: []byte("0123456789abcdef0123456789abcdef")}
		started := time.Now()
		err := applyMigration(ctx, conn, file, migrationTimeouts{lockTimeout: time.Second, statementTimeout: 150 * time.Millisecond, attempts: 4, retryBackoff: time.Second})
		requireSQLState(t, err, "57014", "applyMigration of a slow statement")
		if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
			t.Errorf("statement timeout was retried or unbounded: %s", elapsed)
		}
		if recordedMigrationCount(t, pool, file.version) != 0 {
			t.Error("a timed-out migration was recorded")
		}
	})

	t.Run("a cancelled context stops the retry backoff", func(t *testing.T) {
		pool, conn := migrationRetryFixture(t)
		released := holdTableLock(t, pool, 2*time.Second)
		cancelled, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		err := applyMigration(cancelled, conn, contended(), migrationTimeouts{lockTimeout: 100 * time.Millisecond, statementTimeout: 5 * time.Second, attempts: 10, retryBackoff: 5 * time.Second})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("cancelled retry = %v, want context.DeadlineExceeded", err)
		}
		if err := <-released; err != nil {
			t.Fatal(err)
		}
	})
}
