package pgdialect_test

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
)

func freshExec(t *testing.T) (*pgxpool.Pool, connection.Target, connection.Credential) {
	t.Helper()
	pool := dbtest.FreshTargetPostgres(t)
	if _, err := pool.Exec(context.Background(), "CREATE TABLE exec_t (id int PRIMARY KEY, v text)"); err != nil {
		t.Fatal(err)
	}
	cc := pool.Config().ConnConfig
	target, err := connection.NewTarget(cc.Host, int(cc.Port), cc.Database)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := connection.NewCredential(cc.User, cc.Password)
	if err != nil {
		t.Fatal(err)
	}
	return pool, target, cred
}

func runExec(ctx context.Context, t *testing.T, target connection.Target, cred connection.Credential, exec query.Execution) (cols []query.Column, rows [][]query.CellValue, rowsAffected int64, err error) {
	t.Helper()
	d := pgdialect.New(pgdialect.Options{})
	stream, err := d.Execute(ctx, target, connection.TLSModeDisable, cred, exec)
	if err != nil {
		return nil, nil, 0, err
	}
	cols = stream.Columns()
	for stream.Next() {
		row := stream.Row()
		copied := make([]query.CellValue, len(row))
		copy(copied, row)
		rows = append(rows, copied)
	}
	err = stream.Err()
	rowsAffected = stream.RowsAffected()
	if cerr := stream.Close(); cerr != nil && err == nil {
		err = cerr
	}
	return cols, rows, rowsAffected, err
}

func TestExecuteCloseStopsNext(t *testing.T) {
	t.Parallel()
	_, target, cred := freshExec(t)
	ctx := context.Background()

	d := pgdialect.New(pgdialect.Options{})
	stream, err := d.Execute(ctx, target, connection.TLSModeDisable, cred, query.Execution{
		SQL:   "SELECT 1",
		Class: query.ClassRead,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if stream.Next() {
		t.Fatal("Next() after Close returned a row from an abandoned stream")
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestExecuteReadOnlyRejectsWrite(t *testing.T) {
	t.Parallel()
	pool, target, cred := freshExec(t)

	_, _, _, err := runExec(context.Background(), t, target, cred, query.Execution{
		SQL:   "INSERT INTO exec_t (id, v) VALUES (1, 'smuggled')",
		Class: query.ClassRead,
	})
	var ee *query.ExecError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v (%T), want *query.ExecError", err, err)
	}
	if ee.SQLState != "25006" {
		t.Fatalf("SQLState = %q, want 25006 (read_only_sql_transaction)", ee.SQLState)
	}

	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM exec_t").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("read-only execution left %d row(s)", n)
	}
}

func TestExecuteWriteCommits(t *testing.T) {
	t.Parallel()
	pool, target, cred := freshExec(t)

	_, _, affected, err := runExec(context.Background(), t, target, cred, query.Execution{
		SQL:   "INSERT INTO exec_t (id, v) VALUES (1, 'committed')",
		Class: query.ClassWrite,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if affected != 1 {
		t.Fatalf("RowsAffected = %d, want 1", affected)
	}

	var v string
	if err := pool.QueryRow(context.Background(), "SELECT v FROM exec_t WHERE id = 1").Scan(&v); err != nil {
		t.Fatalf("committed row not visible: %v", err)
	}
	if v != "committed" {
		t.Fatalf("v = %q", v)
	}
}

func TestExecuteWriteRollsBackOnFailure(t *testing.T) {
	t.Parallel()
	pool, target, cred := freshExec(t)
	if _, err := pool.Exec(context.Background(), "INSERT INTO exec_t (id, v) VALUES (1, 'existing')"); err != nil {
		t.Fatal(err)
	}

	_, _, _, err := runExec(context.Background(), t, target, cred, query.Execution{
		SQL:   "INSERT INTO exec_t (id, v) VALUES (2, 'first'), (1, 'dup-secret')",
		Class: query.ClassWrite,
	})
	var ee *query.ExecError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v (%T), want *query.ExecError", err, err)
	}
	if ee.SQLState != "23505" {
		t.Fatalf("SQLState = %q, want 23505 (unique_violation)", ee.SQLState)
	}
	if s := err.Error(); strings.Contains(s, "(1)") || strings.Contains(s, "already exists") || strings.Contains(s, "dup-secret") {
		t.Fatalf("error %q carries PG detail/row data", s)
	}

	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM exec_t WHERE id = 2").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("failed write left partial state (id=2 present)")
	}
}

func TestExecuteDDLCommits(t *testing.T) {
	t.Parallel()
	pool, target, cred := freshExec(t)

	_, _, _, err := runExec(context.Background(), t, target, cred, query.Execution{
		SQL:   "CREATE TABLE exec_ddl (x int)",
		Class: query.ClassDDL,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var reg *string
	if err := pool.QueryRow(context.Background(), "SELECT to_regclass('exec_ddl')::text").Scan(&reg); err != nil {
		t.Fatal(err)
	}
	if reg == nil {
		t.Fatal("DDL did not commit: exec_ddl missing")
	}
}

func TestExecuteCancelStopsQuery(t *testing.T) {
	t.Parallel()
	_, target, cred := freshExec(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, _, _, err := runExec(ctx, t, target, cred, query.Execution{
		SQL:   "SELECT pg_sleep(30)",
		Class: query.ClassRead,
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("canceled execution reported success")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want a context.Canceled wrap", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("cancel took %s — the query ran to completion", elapsed)
	}
}

func TestExecutePreservesDeclaredParameterTypes(t *testing.T) {
	t.Parallel()
	_, target, cred := freshExec(t)
	cols, rows, _, err := runExec(context.Background(), t, target, cred, query.Execution{
		SQL: "SELECT $1 AS n, $2 AS d, $3 AS b, $4 AS ts, $5 AS u, $6 AS dec, $7 AS s, $8::text IS NULL AS is_null",
		Args: []query.TypedValue{
			{Type: query.ParamInteger, Text: "41"},
			{Type: query.ParamDate, Text: "2026-09-30"},
			{Type: query.ParamBoolean, Text: "true"},
			{Type: query.ParamTimestamp, Text: "2026-09-30T12:00:00+09:00"},
			{Type: query.ParamUUID, Text: "3b241101-e2bb-4255-8caf-4136c566a962"},
			{Type: query.ParamDecimal, Text: "12.34"},
			{Type: query.ParamString, Text: "value"},
			{Type: query.ParamNull},
		},
		Class: query.ClassRead,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []query.LogicalType{query.LogicalInt, query.LogicalDate, query.LogicalBool, query.LogicalTimestamptz, query.LogicalUUID, query.LogicalDecimal, query.LogicalString, query.LogicalBool}
	if len(cols) != len(want) || len(rows) != 1 {
		t.Fatalf("columns=%d rows=%d, want %d columns and one row", len(cols), len(rows), len(want))
	}
	for idx, logical := range want {
		if cols[idx].Logical != logical {
			t.Errorf("column %d type=%s, want %s", idx, cols[idx].Logical, logical)
		}
	}
	if rows[0][3].Text != "2026-09-30T03:00:00Z" || !rows[0][7].Bool {
		t.Fatalf("timestamp/null result = %+v", rows[0])
	}
	// Null must infer its type from the integer operand.
	_, nullableRows, _, err := runExec(context.Background(), t, target, cred, query.Execution{
		SQL:   "SELECT 42::bigint = $1 AS comparison",
		Args:  []query.TypedValue{{Type: query.ParamNull}},
		Class: query.ClassRead,
	})
	if err != nil {
		t.Fatalf("contextual null: %v", err)
	}
	if len(nullableRows) != 1 || len(nullableRows[0]) != 1 || nullableRows[0][0].Kind != query.CellNull {
		t.Fatalf("contextual null result = %+v", nullableRows)
	}
}

func TestExecuteBindsTypedArgs(t *testing.T) {
	t.Parallel()
	_, target, cred := freshExec(t)

	_, rows, _, err := runExec(context.Background(), t, target, cred, query.Execution{
		SQL: "SELECT $1::int8 + 1 AS n, $2::text AS s, $3::date AS d, $4::uuid AS u, $5::text IS NULL AS is_null",
		Args: []query.TypedValue{
			{Type: query.ParamInteger, Text: "41"},
			{Type: query.ParamString, Text: "x"},
			{Type: query.ParamDate, Text: "2026-07-19"},
			{Type: query.ParamUUID, Text: "3B241101-E2BB-4255-8CAF-4136C566A962"},
			{Type: query.ParamNull, Text: ""},
		},
		Class: query.ClassRead,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row[0].Kind != query.CellInt || row[0].Text != "42" {
		t.Errorf("n = %+v, want CellInt 42", row[0])
	}
	if row[1].Kind != query.CellString || row[1].Text != "x" {
		t.Errorf("s = %+v, want CellString x", row[1])
	}
	if row[2].Kind != query.CellTemporal || row[2].Text != "2026-07-19" {
		t.Errorf("d = %+v, want CellTemporal 2026-07-19", row[2])
	}
	if row[3].Kind != query.CellString || row[3].Text != "3b241101-e2bb-4255-8caf-4136c566a962" {
		t.Errorf("u = %+v, want canonical lowercase uuid", row[3])
	}
	if row[4].Kind != query.CellBool || row[4].Bool != true {
		t.Errorf("is_null = %+v, want CellBool true", row[4])
	}
}

func TestExecuteCellValueMapping(t *testing.T) {
	t.Parallel()
	_, target, cred := freshExec(t)

	cols, rows, _, err := runExec(context.Background(), t, target, cred, query.Execution{
		SQL: `SELECT true AS b, 42::int2 AS i2, 43::int4 AS i4, 44::int8 AS i8,
			12.34::numeric AS dec, 1.5::float8 AS f8, 2.5::float4 AS f4,
			'txt'::text AS s, decode('deadbeef','hex') AS bin,
			date '2026-07-19' AS d, time '12:34:56' AS tm,
			timestamp '2026-07-19 12:34:56' AS ts,
			timestamptz '2026-07-19 12:34:56+09:00' AS tstz,
			'{"a":1}'::jsonb AS j, '3B241101-E2BB-4255-8CAF-4136C566A962'::uuid AS u,
			'{1,2}'::int4[] AS arr, interval '1 day' AS unk, NULL::text AS nul`,
		Class: query.ClassRead,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]

	wantCols := []struct {
		name    string
		logical query.LogicalType
		dbType  string
	}{
		{"b", query.LogicalBool, "bool"},
		{"i2", query.LogicalInt, "int2"},
		{"i4", query.LogicalInt, "int4"},
		{"i8", query.LogicalInt, "int8"},
		{"dec", query.LogicalDecimal, "numeric"},
		{"f8", query.LogicalFloat, "float8"},
		{"f4", query.LogicalFloat, "float4"},
		{"s", query.LogicalString, "text"},
		{"bin", query.LogicalBytes, "bytea"},
		{"d", query.LogicalDate, "date"},
		{"tm", query.LogicalTime, "time"},
		{"ts", query.LogicalTimestamp, "timestamp"},
		{"tstz", query.LogicalTimestamptz, "timestamptz"},
		{"j", query.LogicalJSON, "jsonb"},
		{"u", query.LogicalUUID, "uuid"},
		{"arr", query.LogicalArray, "_int4"},
		{"unk", query.LogicalUnknown, "interval"},
		{"nul", query.LogicalString, "text"},
	}
	if len(cols) != len(wantCols) {
		t.Fatalf("columns = %d, want %d", len(cols), len(wantCols))
	}
	for idx, want := range wantCols {
		if cols[idx].Name != want.name || cols[idx].Logical != want.logical || cols[idx].DBTypeName != want.dbType {
			t.Errorf("column %d = %+v, want name=%s logical=%s db=%s", idx, cols[idx], want.name, want.logical, want.dbType)
		}
	}

	wantCells := []query.CellValue{
		{Kind: query.CellBool, Bool: true},
		{Kind: query.CellInt, Text: "42"},
		{Kind: query.CellInt, Text: "43"},
		{Kind: query.CellInt, Text: "44"},
		{Kind: query.CellDecimal, Text: "12.34"},
		{Kind: query.CellFloat, Float: 1.5},
		{Kind: query.CellFloat, Float: 2.5},
		{Kind: query.CellString, Text: "txt"},
		{Kind: query.CellBytes, Bytes: []byte{0xde, 0xad, 0xbe, 0xef}},
		{Kind: query.CellTemporal, Text: "2026-07-19"},
		{Kind: query.CellTemporal, Text: "12:34:56"},
		{Kind: query.CellTemporal, Text: "2026-07-19T12:34:56"},
		{Kind: query.CellTemporal, Text: "2026-07-19T03:34:56Z"},
		{Kind: query.CellString, Text: `{"a": 1}`},
		{Kind: query.CellString, Text: "3b241101-e2bb-4255-8caf-4136c566a962"},
		{Kind: query.CellString, Text: "{1,2}"},
		{Kind: query.CellString, Text: "1 day"},
		{Kind: query.CellNull},
	}
	for idx, want := range wantCells {
		got := row[idx]
		if got.Kind != want.Kind {
			t.Errorf("cell %d (%s) kind = %d, want %d", idx, wantCols[idx].name, got.Kind, want.Kind)
			continue
		}
		switch want.Kind {
		case query.CellBool:
			if got.Bool != want.Bool {
				t.Errorf("cell %d (%s) bool = %v", idx, wantCols[idx].name, got.Bool)
			}
		case query.CellFloat:
			if got.Float != want.Float {
				t.Errorf("cell %d (%s) float = %v", idx, wantCols[idx].name, got.Float)
			}
		case query.CellBytes:
			if string(got.Bytes) != string(want.Bytes) {
				t.Errorf("cell %d (%s) bytes = %x", idx, wantCols[idx].name, got.Bytes)
			}
		case query.CellNull:
		default:
			if got.Text != want.Text {
				t.Errorf("cell %d (%s) text = %q, want %q", idx, wantCols[idx].name, got.Text, want.Text)
			}
		}
	}
}

func TestExecuteConnectionFailuresUseBuckets(t *testing.T) {
	t.Parallel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, portStr, _ := net.SplitHostPort(l.Addr().String())
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	target, err := connection.NewTarget("127.0.0.1", port, "db")
	if err != nil {
		t.Fatal(err)
	}
	cred, err := connection.NewCredential("u", "pw-exec-unreachable")
	if err != nil {
		t.Fatal(err)
	}

	d := pgdialect.New(pgdialect.Options{})
	_, execErr := d.Execute(context.Background(), target, connection.TLSModeDisable, cred, query.Execution{
		SQL:   "SELECT 1",
		Class: query.ClassRead,
	})
	assertBucket(t, execErr, connection.TestBucketUnreachable, "pw-exec-unreachable")
}

func TestExecuteWrongPasswordUsesAuthBucket(t *testing.T) {
	t.Parallel()
	_, target, cred := freshExec(t)
	bad, err := connection.NewCredential(cred.User, "definitely-wrong-exec-password")
	if err != nil {
		t.Fatal(err)
	}
	d := pgdialect.New(pgdialect.Options{})
	_, execErr := d.Execute(context.Background(), target, connection.TLSModeDisable, bad, query.Execution{
		SQL:   "SELECT 1",
		Class: query.ClassRead,
	})
	assertBucket(t, execErr, connection.TestBucketAuthFailed, "definitely-wrong-exec-password")
}
