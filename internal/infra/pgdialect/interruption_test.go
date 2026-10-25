package pgdialect_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
)

// slowInsertSQL is an allowlisted write that runs for many seconds before inserting one row.
const slowInsertSQL = "INSERT INTO exec_t (id, v) SELECT 1, count(*)::text FROM generate_series(1, 100000) a, generate_series(1, 100000) b"

// installSlowCommitTrigger makes every COMMIT that inserted into exec_t wait in a deferred constraint trigger.
func installSlowCommitTrigger(t *testing.T, pool *pgxpool.Pool, delaySeconds int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `create function public.slow_commit() returns trigger language plpgsql as $$ begin perform pg_sleep(`+strconv.Itoa(delaySeconds)+`); return null; end $$;
create constraint trigger exec_t_slow_commit after insert on public.exec_t deferrable initially deferred for each row execute function public.slow_commit()`)
	if err != nil {
		t.Fatal(err)
	}
}

// countCommittedRows reports how many exec_t rows another session can see.
func countCommittedRows(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM exec_t").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// interruptionContext starts the scenario's clock once the target is ready.
type interruptionContext func(*testing.T) context.Context

// cancelAfter returns a context cancelled after delay.
func cancelAfter(delay time.Duration) interruptionContext {
	return func(t *testing.T) context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		timer := time.AfterFunc(delay, cancel)
		t.Cleanup(func() { timer.Stop(); cancel() })
		return ctx
	}
}

// expireAfter returns a context whose deadline passes after delay.
func expireAfter(delay time.Duration) interruptionContext {
	return func(t *testing.T) context.Context {
		ctx, cancel := context.WithTimeout(context.Background(), delay)
		t.Cleanup(cancel)
		return ctx
	}
}

// alreadyCancelled returns a context cancelled before execution starts.
func alreadyCancelled(*testing.T) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestInterruptionBeforeCommitIsMarkedRolledBack(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name         string
		startContext interruptionContext
		sql          string
		class        query.StatementClass
		cause        error
	}{
		{"cancelled before dialing", alreadyCancelled, "INSERT INTO exec_t (id, v) VALUES (1, 'a')", query.ClassWrite, context.Canceled},
		{"cancelled during a write statement", cancelAfter(300 * time.Millisecond), slowInsertSQL, query.ClassWrite, context.Canceled},
		{"local deadline during a write statement", expireAfter(500 * time.Millisecond), slowInsertSQL, query.ClassWrite, context.DeadlineExceeded},
		{"cancelled while streaming read rows", cancelAfter(600 * time.Millisecond), "SELECT a, (SELECT count(*) FROM generate_series(1, a * 0 + 3000000)) FROM generate_series(1, 1000) a", query.ClassRead, context.Canceled},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			pool, target, cred := freshExec(t)
			_, _, _, err := runExec(scenario.startContext(t), t, target, cred, query.Execution{SQL: scenario.sql, Class: scenario.class, MaxRows: 100, MaxResultBytes: 1 << 20, TimeoutSeconds: 30})
			if !errors.Is(err, query.ErrInterruptedBeforeCommit) || !errors.Is(err, scenario.cause) {
				t.Fatalf("interruption = %v, want a before-commit %v", err, scenario.cause)
			}
			if committed := countCommittedRows(t, pool); committed != 0 {
				t.Fatalf("interrupted statement committed %d rows", committed)
			}
		})
	}
}

// waitForBlockedCommit observes this fixture's deferred COMMIT waiting on its blocking transaction.
func waitForBlockedCommit(t *testing.T, pool *pgxpool.Pool, blockingPID uint32, executionDone <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		err := pool.QueryRow(ctx, `SELECT EXISTS (
            SELECT 1 FROM pg_stat_activity
            WHERE datname = current_database() AND query = 'COMMIT'
              AND state = 'active' AND wait_event_type = 'Lock' AND wait_event = 'advisory'
              AND $1::integer = ANY(pg_blocking_pids(pid))
        )`, blockingPID).Scan(&blocked)
		if err != nil {
			t.Fatalf("observe deferred COMMIT: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-executionDone:
			t.Fatal("execution ended before the deferred COMMIT barrier")
		case <-ctx.Done():
			t.Fatal("execution did not reach the deferred COMMIT barrier")
		case <-ticker.C:
		}
	}
}

// interruptBlockedCommit interrupts a real COMMIT after observing its deferred trigger's lock wait.
func interruptBlockedCommit(t *testing.T, pool *pgxpool.Pool, target connection.Target, credential connection.Credential, cancelAtCommit bool) error {
	t.Helper()
	fixture, err := os.ReadFile("testdata/commit-interruption.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), string(fixture)); err != nil {
		t.Fatal(err)
	}
	fixtureContext, cancelFixture := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelFixture()
	blocker, err := pool.Begin(fixtureContext)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancelCleanup()
		if err := blocker.Rollback(cleanupContext); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("release deferred COMMIT barrier: %v", err)
		}
	}()
	if _, err := blocker.Exec(fixtureContext, "SELECT pg_advisory_xact_lock('public.exec_t'::regclass::oid::bigint)"); err != nil {
		t.Fatal(err)
	}
	executionContext, cancelExecution := context.WithCancel(context.Background())
	if !cancelAtCommit {
		deadlineContext, cancelDeadline := context.WithTimeout(executionContext, 10*time.Second)
		defer cancelDeadline()
		executionContext = deadlineContext
	}
	executionDone := make(chan struct{})
	var executionError error
	dialect := pgdialect.New(pgdialect.Options{Destinations: dbtest.TargetDestinationPolicy(t), LockTimeout: 30 * time.Second})
	go func() {
		defer close(executionDone)
		stream, err := dialect.Execute(executionContext, target, connection.TLSModeDisable, credential, query.Execution{
			SQL: "INSERT INTO exec_t (id, v) VALUES (1, 'a')", Class: query.ClassWrite,
			MaxRows: 100, MaxResultBytes: 4096, TimeoutSeconds: 30,
		})
		executionError = err
		if err == nil {
			for stream.Next() {
			}
			executionError = stream.Err()
			if err := stream.Close(); executionError == nil {
				executionError = err
			}
		}
	}()
	defer func() {
		cancelExecution()
		select {
		case <-executionDone:
			return
		case <-time.After(5 * time.Second):
		}
		cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()
		if err := blocker.Rollback(cleanupContext); err != nil {
			t.Errorf("release stalled COMMIT barrier: %v", err)
		}
		select {
		case <-executionDone:
		case <-time.After(5 * time.Second):
			t.Error("execution did not stop after releasing its COMMIT barrier")
		}
	}()
	waitForBlockedCommit(t, pool, blocker.Conn().PgConn().PID(), executionDone)
	if err := executionContext.Err(); err != nil {
		t.Fatalf("context ended before observing deferred COMMIT: %v", err)
	}
	if cancelAtCommit {
		cancelExecution()
	}
	select {
	case <-executionDone:
		return executionError
	case <-fixtureContext.Done():
		t.Fatal("execution did not stop within the COMMIT fixture budget")
		return fixtureContext.Err()
	}
}

func TestCommitInterruptionStaysUncertainWhileServerTimeoutIsConfirmed(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name           string
		sql            string
		class          query.StatementClass
		cancelAtCommit bool
		wantCause      error
		wantSQLState   string
	}{
		{"owner cancel while commit runs deferred work", "", query.ClassWrite, true, context.Canceled, ""},
		{"local deadline while commit runs deferred work", "", query.ClassWrite, false, context.DeadlineExceeded, ""},
		{"server statement timeout before commit", slowInsertSQL, query.ClassWrite, false, nil, "57014"},
		{"server statement timeout on a read", "SELECT count(*) FROM generate_series(1, 100000) a, generate_series(1, 100000) b", query.ClassRead, false, nil, "57014"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			pool, target, credential := freshExec(t)
			if scenario.wantCause != nil {
				err := interruptBlockedCommit(t, pool, target, credential, scenario.cancelAtCommit)
				if !errors.Is(err, scenario.wantCause) || errors.Is(err, query.ErrInterruptedBeforeCommit) {
					t.Fatalf("commit interruption = %v, want an uncertain %v", err, scenario.wantCause)
				}
				return
			}
			_, _, _, err := runExec(context.Background(), t, target, credential, query.Execution{
				SQL: scenario.sql, Class: scenario.class, MaxRows: 100, MaxResultBytes: 4096, TimeoutSeconds: 1,
			})
			var executionError *query.ExecError
			if !errors.As(err, &executionError) || executionError.SQLState != scenario.wantSQLState || errors.Is(err, query.ErrInterruptedBeforeCommit) {
				t.Fatalf("server timeout = %v, want confirmed SQLSTATE %s", err, scenario.wantSQLState)
			}
			if committed := countCommittedRows(t, pool); committed != 0 {
				t.Fatalf("timed-out statement committed %d rows", committed)
			}
		})
	}
}

func TestUninterruptedWriteCommitsThroughDeferredWork(t *testing.T) {
	t.Parallel()
	pool, target, cred := freshExec(t)
	installSlowCommitTrigger(t, pool, 1)
	_, _, affected, err := runExec(context.Background(), t, target, cred, query.Execution{SQL: "INSERT INTO exec_t (id, v) VALUES (1, 'a'), (2, 'b')", Class: query.ClassWrite, MaxRows: 100, MaxResultBytes: 4096, TimeoutSeconds: 30})
	if err != nil || affected != 2 {
		t.Fatalf("deferred-work write = %d, %v", affected, err)
	}
	if committed := countCommittedRows(t, pool); committed != 2 {
		t.Fatalf("committed rows = %d, want 2", committed)
	}
}
