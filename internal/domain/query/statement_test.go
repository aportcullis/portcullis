package query_test

import (
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

func TestStatementClassValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		class query.StatementClass
		want  bool
	}{
		{"read", query.ClassRead, true},
		{"write", query.ClassWrite, true},
		{"ddl", query.ClassDDL, true},
		{"empty", query.StatementClass(""), false},
		{"unknown", query.StatementClass("admin"), false},
		{"case-sensitive", query.StatementClass("READ"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.class.Valid(); got != tt.want {
				t.Fatalf("StatementClass(%q).Valid() = %v, want %v", tt.class, got, tt.want)
			}
		})
	}
}
