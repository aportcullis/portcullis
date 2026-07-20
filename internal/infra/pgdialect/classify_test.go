package pgdialect_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/dialecttest"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
)

// The ADR-0002 fixture matrix is the classification contract; the PG adapter
// runs every PG-applicable row (ADR-0001 acceptance gate).
func TestClassificationFixtures(t *testing.T) {
	t.Parallel()
	dialecttest.RunClassification(t, pgdialect.New(pgdialect.Options{}), dialecttest.PG)
}

// A Statement produced by anything but this dialect's ParseSingle must fail
// closed, not be trusted.
func TestClassifyRejectsForeignStatement(t *testing.T) {
	t.Parallel()
	d := pgdialect.New(pgdialect.Options{})
	_, err := d.Classify(foreignStatement{})
	var rej *query.Rejection
	if !errors.As(err, &rej) {
		t.Fatalf("Classify(foreign) = %v, want *query.Rejection", err)
	}
	if rej.Reason != query.RejectNotAllowlisted {
		t.Fatalf("reason = %q, want %q", rej.Reason, query.RejectNotAllowlisted)
	}
}

type foreignStatement struct{}

func (foreignStatement) Text() string { return "SELECT 1" }

// Statement.Text must reproduce the exact input the handle was parsed from —
// the digest and audit paths bind to it.
func TestParseSingleStatementText(t *testing.T) {
	t.Parallel()
	d := pgdialect.New(pgdialect.Options{})
	const sql = "SELECT id, v\nFROM t -- trailing note\nWHERE id = 1"
	st, err := d.ParseSingle(sql)
	if err != nil {
		t.Fatalf("ParseSingle: %v", err)
	}
	if st.Text() != sql {
		t.Fatalf("Text() = %q, want the exact input", st.Text())
	}
}

// Parse failures carry a byte offset only — parser messages can quote the
// input and must not propagate (ADR-0016).
func TestParseSingleFailureCarriesNoInput(t *testing.T) {
	t.Parallel()
	d := pgdialect.New(pgdialect.Options{})
	const secret = "hunter2-super-secret"
	_, err := d.ParseSingle("SELECT WHERE FROM '" + secret)
	if err == nil {
		t.Fatal("ParseSingle accepted garbage")
	}
	var pf *query.ParseFailure
	if !errors.As(err, &pf) {
		t.Fatalf("err = %v (%T), want *query.ParseFailure", err, err)
	}
	if got := err.Error(); strings.Contains(got, secret) || strings.Contains(got, "SELECT WHERE") {
		t.Fatalf("parse error %q leaks input", got)
	}
}
