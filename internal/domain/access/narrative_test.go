package access_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/access"
)

func TestRequestNarrativeSurvivesDraftValidation(t *testing.T) {
	t.Parallel()
	payload, err := access.NewDescribedPayload("  Monthly revenue  ", "Purpose\nReview <script>literal</script>", "select 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Title != "Monthly revenue" || payload.Body != "Purpose\nReview <script>literal</script>" {
		t.Fatalf("narrative lost: %+v", payload)
	}
}

func TestRequestNarrativeLimitsAndLegacyDrafts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, title, body, sql string
		invalid                bool
	}{
		{"legacy", "", "", "select 1", false},
		{"unicode boundary", strings.Repeat("까", 200), strings.Repeat("문", 4000), "select 1", false},
		{"title too long", strings.Repeat("까", 201), "", "select 1", true},
		{"body too long", "Title", strings.Repeat("문", 4001), "select 1", true},
		{"blank provided title", "  ", "Body", "select 1", true},
		{"title newline", "Title\nSpoof", "", "select 1", true},
		{"invalid UTF8", string([]byte{255}), "", "select 1", true},
		{"body NUL", "Title", "a\x00b", "select 1", true},
		{"aggregate budget", "Title", strings.Repeat("b", 4000), strings.Repeat("s", access.MaxPayloadBytes-100), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := access.NewDescribedPayload(tc.title, tc.body, tc.sql, nil)
			if errors.Is(err, access.ErrInvalidPayload) != tc.invalid || (err != nil && !tc.invalid) {
				t.Fatalf("error=%v, invalid=%v", err, tc.invalid)
			}
		})
	}
}

func TestNarrativeChangesInvalidateTheApprovalUnit(t *testing.T) {
	t.Parallel()
	unit := access.ApprovalUnit{SQL: "select 1", Title: "Revenue", Body: "Monthly review"}
	original, err := access.CanonicalPayload(unit)
	if err != nil {
		t.Fatal(err)
	}
	unit.Body = "A different purpose"
	changed, err := access.CanonicalPayload(unit)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(original, changed) {
		t.Fatal("body change retained approval unit")
	}
	unit.Body = "Monthly review"
	unit.Title = "Different title"
	changed, err = access.CanonicalPayload(unit)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(original, changed) {
		t.Fatal("title change retained approval unit")
	}
	unit.Title, unit.Body = "", ""
	legacy, err := access.CanonicalPayload(unit)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(legacy, []byte(`"title"`)) || bytes.Contains(legacy, []byte(`"body"`)) {
		t.Fatal("legacy digest shape changed")
	}
}
