package connection_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

func validTarget(t *testing.T) connection.Target {
	t.Helper()
	target, err := connection.NewTarget("db.example.com", 5432, "appdb")
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestNewConnection(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	target, err := connection.NewTarget("db.example.com", 5432, "appdb")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		id          connection.ConnectionID
		org         identity.OrganizationID
		dbType      connection.DBType
		displayName string
		createdBy   identity.UserID
		wantErr     error
	}{
		{"valid", "c1", "org1", connection.DBTypePostgreSQL, "Prod read replica", "u1", nil},
		{"empty id", "", "org1", connection.DBTypePostgreSQL, "n", "u1", connection.ErrInvalidConnection},
		{"empty org", "c1", "", connection.DBTypePostgreSQL, "n", "u1", connection.ErrInvalidConnection},
		{"empty creator", "c1", "org1", connection.DBTypePostgreSQL, "n", "", connection.ErrInvalidConnection},
		// Only PostgreSQL ships in M1; MySQL/SQLite arrive with M2 adapters.
		{"unsupported db type", "c1", "org1", "mysql", "n", "u1", connection.ErrUnsupportedDBType},
		{"empty display name", "c1", "org1", connection.DBTypePostgreSQL, "", "u1", connection.ErrInvalidDisplayName},
		{"whitespace display name", "c1", "org1", connection.DBTypePostgreSQL, "   ", "u1", connection.ErrInvalidDisplayName},
		{"display name over limit", "c1", "org1", connection.DBTypePostgreSQL, strings.Repeat("x", 257), "u1", connection.ErrInvalidDisplayName},
		{"display name control char", "c1", "org1", connection.DBTypePostgreSQL, "prod\x00", "u1", connection.ErrInvalidDisplayName},
		{"display name bidi override", "c1", "org1", connection.DBTypePostgreSQL, "prod\u202e", "u1", connection.ErrInvalidDisplayName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := connection.New(tt.id, tt.org, tt.dbType, tt.displayName, target, connection.TLSModeVerifyFull, tt.createdBy, now)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("New err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.Fingerprint != target.Fingerprint(connection.DBTypePostgreSQL) {
				t.Error("New must derive the target fingerprint")
			}
			if !got.CreatedAt.Equal(now) || !got.UpdatedAt.Equal(now) {
				t.Error("New must stamp CreatedAt/UpdatedAt from the provided clock")
			}
			if got.IsArchived() {
				t.Error("a new connection must not be archived")
			}
		})
	}
}

func TestArchive(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	conn, err := connection.New("c1", "org1", connection.DBTypePostgreSQL, "prod", validTarget(t), connection.TLSModeVerifyFull, "u1", now)
	if err != nil {
		t.Fatal(err)
	}

	later := now.Add(time.Hour)
	if err := conn.Archive(later); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if !conn.IsArchived() {
		t.Fatal("IsArchived = false after Archive")
	}
	if conn.ArchivedAt == nil || !conn.ArchivedAt.Equal(later) {
		t.Errorf("ArchivedAt = %v, want %v", conn.ArchivedAt, later)
	}
	// Archiving twice is a caller bug surfaced as a sentinel, not a silent no-op:
	// the second call must not move the archive timestamp (audit evidence).
	if err := conn.Archive(later.Add(time.Hour)); !errors.Is(err, connection.ErrAlreadyArchived) {
		t.Fatalf("second Archive err = %v, want ErrAlreadyArchived", err)
	}
	if !conn.ArchivedAt.Equal(later) {
		t.Error("failed re-archive must not move ArchivedAt")
	}
}

func TestNewCredential(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		user     string
		password string
		wantErr  error
	}{
		{"valid", "app_reader", "s3cret", nil},
		// Password may be empty: peer/trust-authenticated targets exist.
		{"empty password allowed", "app_reader", "", nil},
		{"empty user", "", "s3cret", connection.ErrInvalidCredential},
		{"whitespace user", "  ", "s3cret", connection.ErrInvalidCredential},
		{"user with control char", "app\x00reader", "s3cret", connection.ErrInvalidCredential},
		{"user over limit", strings.Repeat("u", 256), "s3cret", connection.ErrInvalidCredential},
		{"password over limit", "app_reader", strings.Repeat("p", 1025), connection.ErrInvalidCredential},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := connection.NewCredential(tt.user, tt.password)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewCredential err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
