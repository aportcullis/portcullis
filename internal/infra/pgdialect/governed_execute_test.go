package pgdialect_test

import (
	"context"
	"errors"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
	"strings"
	"testing"
)

func TestGovernedExecutionRejectsUserOverloadBeforeItRuns(t *testing.T) {
	pool, target, cred := freshExec(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `create domain public.custom_text as text; create function public.lower(public.custom_text) returns text language plpgsql stable as $$ begin raise exception 'overload reached'; end $$; create table public.custom_t(v public.custom_text); insert into public.custom_t values ('secret')`)
	if err != nil {
		t.Fatal(err)
	}
	d := pgdialect.New(pgdialect.Options{})
	stream, err := d.Execute(ctx, target, connection.TLSModeDisable, cred, query.Execution{SQL: "SELECT lower(v) FROM custom_t", Class: query.ClassRead, Governed: true, MaxRows: 100, MaxResultBytes: 4096, TimeoutSeconds: 30})
	if stream != nil {
		_ = stream.Close()
	}
	var rejection *query.Rejection
	if !errors.As(err, &rejection) {
		t.Fatalf("catalog gate did not reject overload: %v", err)
	}
}

func TestGovernedNullRowsCannotBypassDecodedMemoryBudget(t *testing.T) {
	_, target, credential := freshExec(t)
	dialect := pgdialect.New(pgdialect.Options{})
	sql := "SELECT " + strings.TrimSuffix(strings.Repeat("NULL::integer,", 8), ",") + " FROM generate_series(1,20)"
	stream, err := dialect.Execute(context.Background(), target, connection.TLSModeDisable, credential, query.Execution{SQL: sql, Class: query.ClassRead, Governed: true, MaxRows: 20, MaxResultBytes: 4096, TimeoutSeconds: 30})
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
	stream, err := dialect.Execute(context.Background(), target, connection.TLSModeDisable, credential, query.Execution{SQL: "SELECT repeat('x', 29 * 1024 * 1024)", Class: query.ClassRead, Governed: true, MaxRows: 10000, MaxResultBytes: query.MaxSnapshotBytes, TimeoutSeconds: 30})
	if stream != nil {
		_ = stream.Close()
	}
	if err == nil {
		t.Fatal("oversized single cell bypassed protocol allocation limit")
	}
}

func TestGovernedTruncatedReturningWriteCommitsWholeStatement(t *testing.T) {
	pool, target, credential := freshExec(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `create table public.returning_test(id int primary key)`); err != nil {
		t.Fatal(err)
	}
	dialect := pgdialect.New(pgdialect.Options{})
	stream, err := dialect.Execute(ctx, target, connection.TLSModeDisable, credential, query.Execution{SQL: "INSERT INTO returning_test SELECT generate_series(1,5) RETURNING id", Class: query.ClassWrite, Governed: true, MaxRows: 2, MaxResultBytes: 4096, TimeoutSeconds: 30})
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
			stream, err := d.Execute(context.Background(), target, connection.TLSModeDisable, cred, query.Execution{SQL: tc.sql, Class: query.ClassRead, Governed: true, MaxRows: tc.rows, MaxResultBytes: tc.bytes, TimeoutSeconds: 30})
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
