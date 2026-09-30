package query_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

func TestErrorTextsCarryNoInput(t *testing.T) {
	t.Parallel()

	const secret = "SELECT token FROM t WHERE token = 'hunter2'"

	tests := []struct {
		name string
		err  error
	}{
		{"parse failure", &query.ParseFailure{Position: 17}},
		{"rejection", &query.Rejection{Reason: query.RejectNotAllowlisted}},
		{"exec error", &query.ExecError{SQLState: "23505", Message: "duplicate key value violates unique constraint \"t_pkey\"", Position: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msg := tt.err.Error()
			if msg == "" {
				t.Fatal("Error() returned an empty message")
			}
			if strings.Contains(msg, secret) || strings.Contains(msg, "hunter2") {
				t.Fatalf("error text %q leaks input", msg)
			}
		})
	}
}

func TestRejectionCarriesReason(t *testing.T) {
	t.Parallel()

	rej := &query.Rejection{Reason: query.RejectTxnControl}
	if !strings.Contains(rej.Error(), string(query.RejectTxnControl)) {
		t.Fatalf("Rejection.Error() = %q, want it to name the reason %q", rej.Error(), query.RejectTxnControl)
	}

	var target *query.Rejection
	if !errors.As(error(rej), &target) {
		t.Fatal("errors.As failed to unwrap *Rejection")
	}
}

func TestParseFailureCarriesPosition(t *testing.T) {
	t.Parallel()

	pf := &query.ParseFailure{Position: 42}
	if !strings.Contains(pf.Error(), "42") {
		t.Fatalf("ParseFailure.Error() = %q, want it to include the byte offset", pf.Error())
	}
}

func TestExecErrorCarriesSQLState(t *testing.T) {
	t.Parallel()

	ee := &query.ExecError{SQLState: "25006", Message: "cannot execute INSERT in a read-only transaction"}
	if !strings.Contains(ee.Error(), "25006") {
		t.Fatalf("ExecError.Error() = %q, want it to include the SQLSTATE", ee.Error())
	}
}

func TestExecErrorStringExcludesMessage(t *testing.T) {
	t.Parallel()

	ee := &query.ExecError{SQLState: "22P02", Message: `invalid input syntax for type integer: "hunter2-value"`}
	if strings.Contains(ee.Error(), "hunter2-value") || strings.Contains(ee.Error(), "invalid input syntax") {
		t.Fatalf("ExecError.Error() = %q leaks the primary message", ee.Error())
	}
}
