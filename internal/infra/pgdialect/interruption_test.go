package pgdialect_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/query"
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

// uninterrupted returns a context that never ends.
func uninterrupted(*testing.T) context.Context { return context.Background() }

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
			_, _, _, err := runExec(scenario.startContext(t), t, target, cred, query.Execution{SQL: scenario.sql, Class: scenario.class, Governed: true, MaxRows: 100, MaxResultBytes: 1 << 20, TimeoutSeconds: 30})
			if !errors.Is(err, query.ErrInterruptedBeforeCommit) || !errors.Is(err, scenario.cause) {
				t.Fatalf("interruption = %v, want a before-commit %v", err, scenario.cause)
			}
			if committed := countCommittedRows(t, pool); committed != 0 {
				t.Fatalf("interrupted statement committed %d rows", committed)
			}
		})
	}
}

func TestCommitInterruptionStaysUncertainWhileServerTimeoutIsConfirmed(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name           string
		startContext   interruptionContext
		sql            string
		class          query.StatementClass
		slowCommit     bool
		timeoutSeconds int
		wantCause      error
		wantSQLState   string
	}{
		{"owner cancel while commit runs deferred work", cancelAfter(500 * time.Millisecond), "INSERT INTO exec_t (id, v) VALUES (1, 'a')", query.ClassWrite, true, 30, context.Canceled, ""},
		{"local deadline while commit runs deferred work", expireAfter(500 * time.Millisecond), "INSERT INTO exec_t (id, v) VALUES (1, 'a')", query.ClassWrite, true, 30, context.DeadlineExceeded, ""},
		{"server statement timeout before commit", uninterrupted, slowInsertSQL, query.ClassWrite, false, 1, nil, "57014"},
		{"server statement timeout on a read", uninterrupted, "SELECT count(*) FROM generate_series(1, 100000) a, generate_series(1, 100000) b", query.ClassRead, false, 1, nil, "57014"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			pool, target, cred := freshExec(t)
			if scenario.slowCommit {
				installSlowCommitTrigger(t, pool, 4)
			}
			_, _, _, err := runExec(scenario.startContext(t), t, target, cred, query.Execution{SQL: scenario.sql, Class: scenario.class, Governed: true, MaxRows: 100, MaxResultBytes: 4096, TimeoutSeconds: scenario.timeoutSeconds})
			if scenario.wantCause != nil {
				if !errors.Is(err, scenario.wantCause) || errors.Is(err, query.ErrInterruptedBeforeCommit) {
					t.Fatalf("commit interruption = %v, want an uncertain %v", err, scenario.wantCause)
				}
				return
			}
			var execErr *query.ExecError
			if !errors.As(err, &execErr) || execErr.SQLState != scenario.wantSQLState || errors.Is(err, query.ErrInterruptedBeforeCommit) {
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
	_, _, affected, err := runExec(context.Background(), t, target, cred, query.Execution{SQL: "INSERT INTO exec_t (id, v) VALUES (1, 'a'), (2, 'b')", Class: query.ClassWrite, Governed: true, MaxRows: 100, MaxResultBytes: 4096, TimeoutSeconds: 30})
	if err != nil || affected != 2 {
		t.Fatalf("deferred-work write = %d, %v", affected, err)
	}
	if committed := countCommittedRows(t, pool); committed != 2 {
		t.Fatalf("committed rows = %d, want 2", committed)
	}
}
