package pgdialect_test

import (
	"context"
	"errors"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
	"time"
)

func TestGovernedExecutionRejectsUserOverloadBeforeItRuns(t *testing.T) {
	pool, target, cred := freshExec(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `create domain public.custom_text as text; create function public.lower(public.custom_text) returns text language plpgsql stable as $$ begin raise exception 'overload reached'; end $$; create table public.custom_t(v public.custom_text); insert into public.custom_t values ('secret')`)
	if err != nil {
		t.Fatal(err)
	}
	d := pgdialect.New(pgdialect.Options{})
	stream, err := d.Execute(ctx, target, connection.TLSModeDisable, cred, query.Execution{SQL: "SELECT lower(v) FROM custom_t", Class: query.ClassRead, MaxRows: 100, MaxResultBytes: 4096, TimeoutSeconds: 30})
	if stream != nil {
		_ = stream.Close()
	}
	var rejection *query.Rejection
	if !errors.As(err, &rejection) {
		t.Fatalf("catalog gate did not reject overload: %v", err)
	}
}

func TestGovernedExecutionRejectsUserOperatorInSubqueryComparison(t *testing.T) {
	pool, target, cred := freshExec(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `create domain public.custom_int as int; create function public.boom_eq(public.custom_int, int) returns boolean language plpgsql stable as $$ begin raise exception 'operator reached'; end $$; create operator public.= (leftarg = public.custom_int, rightarg = int, function = public.boom_eq); create table public.custom_ids(v public.custom_int); insert into public.custom_ids values (1)`)
	if err != nil {
		t.Fatal(err)
	}
	d := pgdialect.New(pgdialect.Options{})
	for _, sql := range []string{"SELECT v FROM custom_ids WHERE v = ANY (SELECT 1)", "SELECT v FROM custom_ids WHERE v IN (SELECT 1)"} {
		stream, err := d.Execute(ctx, target, connection.TLSModeDisable, cred, query.Execution{SQL: sql, Class: query.ClassRead, MaxRows: 100, MaxResultBytes: 4096, TimeoutSeconds: 30})
		if stream != nil {
			_ = stream.Close()
		}
		var rejection *query.Rejection
		if !errors.As(err, &rejection) {
			t.Fatalf("catalog gate did not reject user operator in %q: %v", sql, err)
		}
	}
}

func TestGovernedUnqualifiedDDLCreatesObjectInPublicSchema(t *testing.T) {
	pool, target, cred := freshExec(t)
	ctx := context.Background()
	_, _, _, err := runExec(ctx, t, target, cred, query.Execution{SQL: "CREATE TABLE governed_ddl (x int)", Class: query.ClassDDL, MaxRows: 100, MaxResultBytes: 4096, TimeoutSeconds: 30})
	if err != nil {
		t.Fatalf("governed unqualified DDL failed: %v", err)
	}
	var createdTable *string
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.governed_ddl')::text").Scan(&createdTable); err != nil {
		t.Fatal(err)
	}
	if createdTable == nil {
		t.Fatal("governed DDL did not create public.governed_ddl")
	}
}

// lockScenario holds heldLock in another transaction while a governed execution runs statement.
type lockScenario struct {
	name      string
	heldLock  string
	statement string
	class     query.StatementClass
}

// executeWhileLockHeld runs one governed statement with a 1s lock timeout while a separate transaction holds the scenario's lock.
func executeWhileLockHeld(t *testing.T, scenario lockScenario) (time.Duration, error) {
	t.Helper()
	pool, target, cred := freshExec(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "INSERT INTO exec_t (id, v) VALUES (1, 'a'), (2, 'b')"); err != nil {
		t.Fatal(err)
	}
	lockHolder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lockHolder.Rollback(ctx) }()
	if scenario.heldLock != "" {
		if _, err := lockHolder.Exec(ctx, scenario.heldLock); err != nil {
			t.Fatal(err)
		}
	}
	dialect := pgdialect.New(pgdialect.Options{LockTimeout: time.Second})
	started := time.Now()
	stream, err := dialect.Execute(ctx, target, connection.TLSModeDisable, cred, query.Execution{SQL: scenario.statement, Class: scenario.class, MaxRows: 100, MaxResultBytes: 4096, TimeoutSeconds: 30})
	if err == nil {
		for stream.Next() {
			_ = stream.Row()
		}
		err = stream.Err()
		if closeErr := stream.Close(); err == nil {
			err = closeErr
		}
	}
	return time.Since(started), err
}

func TestGovernedExecutionStopsWaitingForConflictingLockAtLockTimeout(t *testing.T) {
	for _, scenario := range []lockScenario{
		{name: "read behind access exclusive table lock", heldLock: "LOCK TABLE exec_t IN ACCESS EXCLUSIVE MODE", statement: "SELECT id FROM exec_t", class: query.ClassRead},
		{name: "update behind row lock", heldLock: "SELECT id FROM exec_t WHERE id = 1 FOR UPDATE", statement: "UPDATE exec_t SET v = 'z' WHERE id = 1", class: query.ClassWrite},
		{name: "insert behind share table lock", heldLock: "LOCK TABLE exec_t IN SHARE MODE", statement: "INSERT INTO exec_t (id, v) VALUES (9, 'z')", class: query.ClassWrite},
		{name: "truncate behind reader", heldLock: "SELECT id FROM exec_t", statement: "TRUNCATE TABLE exec_t", class: query.ClassDDL},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			waited, err := executeWhileLockHeld(t, scenario)
			var execErr *query.ExecError
			if !errors.As(err, &execErr) || execErr.SQLState != "55P03" {
				t.Fatalf("lock wait error = %v, want SQLSTATE 55P03 lock_not_available", err)
			}
			if waited > 10*time.Second {
				t.Fatalf("lock wait took %s, want about the 1s lock timeout", waited)
			}
		})
	}
}

func TestGovernedExecutionProceedsAlongsideCompatibleLocks(t *testing.T) {
	for _, scenario := range []lockScenario{
		{name: "read with no other transaction", statement: "SELECT id FROM exec_t", class: query.ClassRead},
		{name: "read beside row update", heldLock: "UPDATE exec_t SET v = 'held' WHERE id = 1", statement: "SELECT id FROM exec_t", class: query.ClassRead},
		{name: "read beside share table lock", heldLock: "LOCK TABLE exec_t IN SHARE MODE", statement: "SELECT id FROM exec_t", class: query.ClassRead},
		{name: "update of another row beside row lock", heldLock: "SELECT id FROM exec_t WHERE id = 1 FOR UPDATE", statement: "UPDATE exec_t SET v = 'z' WHERE id = 2", class: query.ClassWrite},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if _, err := executeWhileLockHeld(t, scenario); err != nil {
				t.Fatalf("compatible lock blocked execution: %v", err)
			}
		})
	}
}

func TestGovernedNullRowsCannotBypassDecodedMemoryBudget(t *testing.T) {
	_, target, credential := freshExec(t)
	dialect := pgdialect.New(pgdialect.Options{})
	sql := "SELECT " + strings.TrimSuffix(strings.Repeat("NULL::integer,", 8), ",") + " FROM generate_series(1,20)"
	stream, err := dialect.Execute(context.Background(), target, connection.TLSModeDisable, credential, query.Execution{SQL: sql, Class: query.ClassRead, MaxRows: 20, MaxResultBytes: 4096, TimeoutSeconds: 30})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	count := 0
	for stream.Next() {
		count++
	}
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	if count == 20 || !stream.Truncated() {
		t.Fatalf("NULL cells bypassed byte admission: rows=%d truncated=%v", count, stream.Truncated())
	}
}

func TestGovernedOversizedCellRefusesProtocolBeforeReturningRows(t *testing.T) {
	_, target, credential := freshExec(t)
	dialect := pgdialect.New(pgdialect.Options{})
	stream, err := dialect.Execute(context.Background(), target, connection.TLSModeDisable, credential, query.Execution{SQL: "SELECT repeat('x', 29 * 1024 * 1024)", Class: query.ClassRead, MaxRows: 10000, MaxResultBytes: query.MaxSnapshotBytes, TimeoutSeconds: 30})
	if stream != nil {
		_ = stream.Close()
	}
	if !errors.Is(err, query.ErrResponseLimit) {
		t.Fatalf("oversized cell must report a local response limit, got %v", err)
	}
}

func TestGovernedTruncatedReturningWriteCommitsWholeStatement(t *testing.T) {
	pool, target, credential := freshExec(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `create table public.returning_test(id int primary key)`); err != nil {
		t.Fatal(err)
	}
	dialect := pgdialect.New(pgdialect.Options{})
	stream, err := dialect.Execute(ctx, target, connection.TLSModeDisable, credential, query.Execution{SQL: "INSERT INTO returning_test SELECT generate_series(1,5) RETURNING id", Class: query.ClassWrite, MaxRows: 2, MaxResultBytes: 4096, TimeoutSeconds: 30})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	count := 0
	for stream.Next() {
		count++
	}
	if stream.Err() != nil || count != 2 || !stream.Truncated() || stream.RowsAffected() != 5 {
		t.Fatalf("truncated write rows=%d affected=%d error=%v", count, stream.RowsAffected(), stream.Err())
	}
	var persisted int
	if err := pool.QueryRow(ctx, `select count(*) from public.returning_test`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != 5 {
		t.Fatalf("partial commit: %d", persisted)
	}
}

// governedDrain is what a governed execution delivered once its stream was fully consumed.
type governedDrain struct {
	rowCount     int
	truncated    bool
	rowsAffected int64
	elapsed      time.Duration
	err          error
}

// drainGovernedRead runs one governed read and consumes every row the stream offers.
func drainGovernedRead(t *testing.T, target connection.Target, cred connection.Credential, sql string, maxRows int, maxResultBytes int64) governedDrain {
	t.Helper()
	dialect := pgdialect.New(pgdialect.Options{})
	started := time.Now()
	stream, err := dialect.Execute(context.Background(), target, connection.TLSModeDisable, cred, query.Execution{SQL: sql, Class: query.ClassRead, MaxRows: maxRows, MaxResultBytes: maxResultBytes, TimeoutSeconds: 30})
	if err != nil {
		return governedDrain{elapsed: time.Since(started), err: err}
	}
	defer func() { _ = stream.Close() }()
	drained := governedDrain{}
	for stream.Next() {
		drained.rowCount++
	}
	drained.err = stream.Err()
	drained.truncated = stream.Truncated()
	drained.rowsAffected = stream.RowsAffected()
	drained.elapsed = time.Since(started)
	return drained
}

func TestGovernedTruncatedReadStopsAtSnapshotCeiling(t *testing.T) {
	pool, target, cred := freshExec(t)
	for _, scenario := range []struct {
		name           string
		sql            string
		maxRows        int
		maxResultBytes int64
	}{
		// Set-returning functions in the select list stream rows; in FROM they materialize before the first row.
		{name: "row ceiling on a huge series", sql: "SELECT generate_series(1, 2000000000) AS g", maxRows: 100, maxResultBytes: 1 << 20},
		{name: "byte ceiling on wide rows", sql: "SELECT repeat('x', 1000), generate_series(1, 2000000000)", maxRows: 10000, maxResultBytes: 8192},
		{name: "row ceiling before a later division error", sql: "SELECT 1 / (g - 50000000) FROM (SELECT generate_series(1, 100000000) AS g) AS series", maxRows: 100, maxResultBytes: 1 << 20},
		{name: "row ceiling on a filtered series", sql: "SELECT g FROM (SELECT generate_series(1, 2000000000) AS g) AS series WHERE g % 2 = 0", maxRows: 50, maxResultBytes: 1 << 20},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			drained := drainGovernedRead(t, target, cred, scenario.sql, scenario.maxRows, scenario.maxResultBytes)
			if drained.err != nil || !drained.truncated || drained.rowCount == 0 || drained.rowCount > scenario.maxRows {
				t.Fatalf("truncated read rows=%d truncated=%v error=%v", drained.rowCount, drained.truncated, drained.err)
			}
			if drained.rowsAffected != int64(drained.rowCount) {
				t.Fatalf("truncated read reported %d rows affected, want the %d delivered rows", drained.rowsAffected, drained.rowCount)
			}
			if drained.elapsed > 10*time.Second {
				t.Fatalf("truncated read took %s, want it to stop at the ceiling", drained.elapsed)
			}
			assertNoActiveTargetQuery(t, pool, scenario.sql)
		})
	}
}

func TestGovernedReadBelowSnapshotCeilingKeepsCompleteOutcome(t *testing.T) {
	_, target, cred := freshExec(t)
	for _, scenario := range []struct {
		name         string
		sql          string
		wantRows     int
		wantSQLState string
	}{
		{name: "small result", sql: "SELECT g FROM generate_series(1, 3) g", wantRows: 3},
		{name: "exactly the row ceiling", sql: "SELECT g FROM generate_series(1, 100) g", wantRows: 100},
		{name: "empty result", sql: "SELECT g FROM generate_series(1, 0) g", wantRows: 0},
		{name: "division error before the ceiling", sql: "SELECT 1 / (g - 50) FROM generate_series(1, 200) g", wantSQLState: "22012"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			drained := drainGovernedRead(t, target, cred, scenario.sql, 100, 1<<20)
			if scenario.wantSQLState != "" {
				var execErr *query.ExecError
				if !errors.As(drained.err, &execErr) || execErr.SQLState != scenario.wantSQLState {
					t.Fatalf("read error = %v, want SQLSTATE %s", drained.err, scenario.wantSQLState)
				}
				return
			}
			if drained.err != nil || drained.truncated || drained.rowCount != scenario.wantRows || drained.rowsAffected != int64(scenario.wantRows) {
				t.Fatalf("complete read rows=%d affected=%d truncated=%v error=%v, want %d rows", drained.rowCount, drained.rowsAffected, drained.truncated, drained.err, scenario.wantRows)
			}
		})
	}
}

// assertNoActiveTargetQuery waits briefly for the target to stop running sql after the stream ended.
func assertNoActiveTargetQuery(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var activeCount int
		if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM pg_stat_activity WHERE state = 'active' AND query = $1", sql).Scan(&activeCount); err != nil {
			t.Fatal(err)
		}
		if activeCount == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("target still runs %q after the truncated stream ended", sql)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestGovernedExecutionCapsRowsAndBytesBeforeDecoding(t *testing.T) {
	_, target, cred := freshExec(t)
	d := pgdialect.New(pgdialect.Options{})
	for _, tc := range []struct {
		name, sql string
		rows      int
		bytes     int64
		want      int
	}{
		{"row cap", "SELECT generate_series(1, 5)", 2, 4096, 2},
		{"byte cap", "SELECT repeat('x',1000) FROM generate_series(1,5)", 100, 2100, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream, err := d.Execute(context.Background(), target, connection.TLSModeDisable, cred, query.Execution{SQL: tc.sql, Class: query.ClassRead, MaxRows: tc.rows, MaxResultBytes: tc.bytes, TimeoutSeconds: 30})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stream.Close() }()
			count := 0
			for stream.Next() {
				count++
			}
			if err := stream.Err(); err != nil {
				t.Fatal(err)
			}
			if count != tc.want || !stream.Truncated() {
				t.Fatalf("rows=%d truncated=%v", count, stream.Truncated())
			}
		})
	}
}

func TestGovernedExecutionRunsGrammarRewrittenCatalogFunctions(t *testing.T) {
	_, target, credential := freshExec(t)
	ctx := context.Background()
	for _, scenario := range []struct{ sql, want string }{
		{sql: "SELECT extract(year from timestamp '2026-10-04 12:00')", want: "2026"},
		{sql: "SELECT substring('portcullis' from 1 for 4) || position('c' in 'portcullis')", want: "port5"},
		{sql: "SELECT trim(both 'x' from 'xxgatexx') || overlay('gate' placing 'l' from 1 for 1)", want: "gatelate"},
		{sql: "SELECT (timestamptz '2026-10-04 00:00+00' AT TIME ZONE 'Asia/Seoul')::text", want: "2026-10-04 09:00:00"},
		{sql: "SELECT ('a%b' LIKE 'a!%b' ESCAPE '!')::text || ('abc' SIMILAR TO 'a%')::text", want: "truetrue"},
	} {
		t.Run(scenario.sql, func(t *testing.T) {
			_, rows, _, err := runExec(ctx, t, target, credential, query.Execution{SQL: scenario.sql, Class: query.ClassRead, MaxRows: 10, MaxResultBytes: 4096, TimeoutSeconds: 30})
			if err != nil {
				t.Fatalf("grammar-rewritten built-in refused: %v", err)
			}
			if len(rows) != 1 || len(rows[0]) != 1 || rows[0][0].Text != scenario.want {
				t.Fatalf("rows = %+v, want %q", rows, scenario.want)
			}
		})
	}
}

func TestGovernedExecutionRejectsUserFunctionPlantedInPgCatalog(t *testing.T) {
	pool, target, credential := freshExec(t)
	ctx := context.Background()
	// The planted overloads use signatures PostgreSQL would not pick for these calls: the gate must refuse any untrusted candidate of the name, not only the one overload resolution selects (ADR-0021).
	_, err := pool.Exec(ctx, `create function pg_catalog.extract(text, int) returns numeric language plpgsql stable as $$ begin raise exception 'planted extract reached'; end $$;
create function pg_catalog.btrim(text, int) returns text language plpgsql stable as $$ begin raise exception 'planted btrim reached'; end $$;
create function pg_catalog.timezone(int, text) returns text language plpgsql stable as $$ begin raise exception 'planted timezone reached'; end $$`)
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"SELECT extract(year from now())",
		"SELECT trim(both 'x' from 'xax')",
		"SELECT now() AT TIME ZONE 'UTC'",
	} {
		t.Run(sql, func(t *testing.T) {
			_, _, _, err := runExec(ctx, t, target, credential, query.Execution{SQL: sql, Class: query.ClassRead, MaxRows: 10, MaxResultBytes: 4096, TimeoutSeconds: 30})
			var rejection *query.Rejection
			if !errors.As(err, &rejection) {
				t.Fatalf("catalog gate did not reject a planted pg_catalog overload: %v", err)
			}
		})
	}
}

// executeGovernedDDL runs one governed DDL statement and drains its stream.
func executeGovernedDDL(ctx context.Context, t *testing.T, target connection.Target, credential connection.Credential, sql string) error {
	t.Helper()
	_, _, _, err := runExec(ctx, t, target, credential, query.Execution{SQL: sql, Class: query.ClassDDL, MaxRows: 100, MaxResultBytes: 4096, TimeoutSeconds: 30})
	return err
}

func TestGovernedExecutionRejectsUserExclusionOperatorOrAccessMethodBeforeItRuns(t *testing.T) {
	pool, target, credential := freshExec(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `create function public.boom_cmp(int, int) returns boolean language sql immutable as 'select false';
create operator public.&& (leftarg = int, rightarg = int, function = public.boom_cmp);
create operator public.<@ (leftarg = int, rightarg = int, function = public.boom_cmp);
create operator public.@> (leftarg = int, rightarg = int, function = public.boom_cmp);
create access method evil_am type index handler bthandler;
create access method evil_tam type table handler heap_tableam_handler`)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct{ sql, createdTable string }{
		{sql: "CREATE TABLE excl_one (p int, EXCLUDE (p WITH &&))", createdTable: "excl_one"},
		{sql: "ALTER TABLE exec_t ADD CONSTRAINT excl_alter EXCLUDE (id WITH &&)"},
		{sql: "CREATE TABLE excl_hash (p int, EXCLUDE USING hash (p WITH <@))", createdTable: "excl_hash"},
		{sql: "CREATE TABLE excl_pair (p int, q int, EXCLUDE (p WITH =, q WITH @>))", createdTable: "excl_pair"},
		{sql: "CREATE INDEX evil_index ON exec_t USING evil_am (v)", createdTable: "evil_index"},
		{sql: "CREATE TABLE evil_table (v int) USING evil_tam", createdTable: "evil_table"},
	} {
		t.Run(scenario.sql, func(t *testing.T) {
			err := executeGovernedDDL(ctx, t, target, credential, scenario.sql)
			var rejection *query.Rejection
			if !errors.As(err, &rejection) {
				t.Fatalf("catalog gate did not reject user exclusion operator: %v", err)
			}
			if scenario.createdTable == "" {
				return
			}
			var created *string
			if err := pool.QueryRow(ctx, "SELECT to_regclass($1)::text", "public."+scenario.createdTable).Scan(&created); err != nil {
				t.Fatal(err)
			}
			if created != nil {
				t.Fatalf("rejected statement still created %s", *created)
			}
		})
	}
}

func TestGovernedExecutionAdmitsBuiltinIndexMethodsAndExclusionOperators(t *testing.T) {
	_, target, credential := freshExec(t)
	ctx := context.Background()
	for _, sql := range []string{
		"CREATE TABLE excl_eq (p int, EXCLUDE (p WITH =))",
		"CREATE TABLE excl_range (r int4range, EXCLUDE USING gist (r WITH &&))",
		"CREATE INDEX exec_t_hash ON exec_t USING hash (v)",
		"CREATE TABLE heap_t (v int) USING heap",
	} {
		t.Run(sql, func(t *testing.T) {
			if err := executeGovernedDDL(ctx, t, target, credential, sql); err != nil {
				t.Fatalf("built-in method or operator refused: %v", err)
			}
		})
	}
}
