package pgdialect_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
)

func TestDefaultExecutionIsGovernedBeforeDialing(t *testing.T) {
	t.Parallel()
	// Nothing listens here: an execution that reaches the dial reports a connection failure instead of a rejection.
	unreachable, err := connection.NewTarget("127.0.0.1", 1, "postgres")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := connection.NewCredential("portcullis", "unused")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name string
		exec query.Execution
	}{
		{"no limits at all", query.Execution{SQL: "SELECT 1", Class: query.ClassRead}},
		{"class differs from the statement", query.Execution{SQL: "DELETE FROM exec_t", Class: query.ClassRead, MaxRows: 10, MaxResultBytes: 4096, TimeoutSeconds: 1}},
		{"row limit above the ceiling", query.Execution{SQL: "SELECT 1", Class: query.ClassRead, MaxRows: 10_001, MaxResultBytes: 4096, TimeoutSeconds: 1}},
		{"function outside the allowlist", query.Execution{SQL: "SELECT pg_sleep(1)", Class: query.ClassRead, MaxRows: 10, MaxResultBytes: 4096, TimeoutSeconds: 1}},
		{"two statements", query.Execution{SQL: "SELECT 1; SELECT 2", Class: query.ClassRead, MaxRows: 10, MaxResultBytes: 4096, TimeoutSeconds: 1}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			stream, err := pgdialect.New(pgdialect.Options{}).Execute(context.Background(), unreachable, connection.TLSModeDisable, credential, scenario.exec)
			if stream != nil {
				_ = stream.Close()
			}
			var dialFailure *connection.TestError
			if err == nil || errors.As(err, &dialFailure) {
				t.Fatalf("default execution reached the target: %v", err)
			}
		})
	}
}

func TestDefaultExecutionRunsWithGovernedSession(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name     string
		exec     query.Execution
		wantRows int
	}{
		{"governed read", query.Execution{SQL: "SELECT id FROM exec_t ORDER BY id", Class: query.ClassRead, MaxRows: 10, MaxResultBytes: 4096, TimeoutSeconds: 5}, 2},
		{"governed read capped by its row limit", query.Execution{SQL: "SELECT id FROM exec_t ORDER BY id", Class: query.ClassRead, MaxRows: 1, MaxResultBytes: 4096, TimeoutSeconds: 5}, 1},
		{"governed write", query.Execution{SQL: "UPDATE exec_t SET v = 'z' WHERE id = 1", Class: query.ClassWrite, MaxRows: 10, MaxResultBytes: 4096, TimeoutSeconds: 5}, 0},
		{"explicitly ungoverned adapter test", query.Execution{SQL: "SELECT pg_sleep(0)", Class: query.ClassRead, Ungoverned: true}, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			pool, target, cred := freshExec(t)
			if _, err := pool.Exec(context.Background(), "INSERT INTO exec_t (id, v) VALUES (1, 'a'), (2, 'b')"); err != nil {
				t.Fatal(err)
			}
			_, rows, _, err := runExec(context.Background(), t, target, cred, scenario.exec)
			if err != nil || len(rows) != scenario.wantRows {
				t.Fatalf("rows = %d, err = %v; want %d rows", len(rows), err, scenario.wantRows)
			}
		})
	}
}
