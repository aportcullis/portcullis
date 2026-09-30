package postgres_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestErrorLogFieldsLogsStructuralFieldsNotMessage(t *testing.T) {
	t.Parallel()
	fields := pg.ErrorLogFields(&pgconn.PgError{
		Code:           "23505",
		Message:        "duplicate key value violates secret-in-message",
		Detail:         "Key (email)=(secret@example.com) already exists.",
		TableName:      "users",
		ConstraintName: "users_email_key",
	})
	got := fmt.Sprint(fields...)
	if !strings.Contains(got, "23505") || !strings.Contains(got, "users_email_key") || !strings.Contains(got, "users") {
		t.Errorf("should log the SQLSTATE code + structural identifiers, got %v", fields)
	}

	if strings.Contains(got, "secret-in-message") || strings.Contains(got, "secret@example.com") {
		t.Errorf("must not log Message or Detail, got %v", fields)
	}
}

func TestErrorLogFieldsHidesConnectionSecret(t *testing.T) {
	t.Parallel()
	fields := pg.ErrorLogFields(errors.New("failed to connect: dial tcp: password=topsecret host=internal-db"))
	got := fmt.Sprint(fields...)
	if strings.Contains(got, "topsecret") || strings.Contains(got, "internal-db") {
		t.Errorf("a connection error must not leak its message, got %v", fields)
	}
	if !strings.Contains(got, "error_type") {
		t.Errorf("a non-PgError should be logged by type only, got %v", fields)
	}
}
