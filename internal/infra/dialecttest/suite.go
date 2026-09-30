package dialecttest

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// Dialect is the slice of a dialect adapter the classification suite exercises; every engine's adapter satisfies it structurally.
type Dialect interface {
	ParseSingle(sql string) (query.Statement, error)
	Classify(st query.Statement) (query.StatementClass, error)
}

// RunClassification runs every fixture applicable to engine through ParseSingle→Classify and asserts the pinned outcome, including the exact reject reason — fail-closed means rejecting for the *right* reason, not just rejecting.
func RunClassification(t *testing.T, d Dialect, engine Engine) {
	t.Helper()
	for _, f := range Fixtures() {
		if !f.AppliesTo(engine) {
			continue
		}
		t.Run(fmt.Sprintf("fixture_%02d", f.N), func(t *testing.T) {
			t.Parallel()
			class, err := classifyOnce(d, f.SQL)

			if f.Expect.Reject {
				if err == nil {
					t.Fatalf("fixture #%d %q: classified as %q, want reject (%s)", f.N, f.SQL, class, f.Expect.Reason)
				}
				reason, ok := rejectReason(err)
				if !ok {
					t.Fatalf("fixture #%d %q: error %v is not a classification rejection", f.N, f.SQL, err)
				}
				if reason != f.Expect.Reason {
					t.Fatalf("fixture #%d %q: reject reason = %q, want %q", f.N, f.SQL, reason, f.Expect.Reason)
				}
				return
			}

			if err != nil {
				t.Fatalf("fixture #%d %q: unexpected error: %v", f.N, f.SQL, err)
			}
			if class != f.Expect.Class {
				t.Fatalf("fixture #%d %q: class = %q, want %q", f.N, f.SQL, class, f.Expect.Class)
			}
		})
	}
}

func classifyOnce(d Dialect, sql string) (query.StatementClass, error) {
	st, err := d.ParseSingle(sql)
	if err != nil {
		return "", err
	}
	return d.Classify(st)
}

// rejectReason maps the dialect's refusal errors onto the fixture vocabulary: parse-level sentinels for empty/multi input, *query.Rejection for everything the classifier refuses.
func rejectReason(err error) (query.RejectReason, bool) {
	switch {
	case errors.Is(err, query.ErrEmptyStatement):
		return query.RejectEmpty, true
	case errors.Is(err, query.ErrMultipleStatements):
		return query.RejectMultiStatement, true
	}
	var rej *query.Rejection
	if errors.As(err, &rej) {
		return rej.Reason, true
	}
	return "", false
}
