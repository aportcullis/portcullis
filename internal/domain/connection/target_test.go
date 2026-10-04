package connection_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/connection"
)

func TestNewTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		host     string
		port     int
		database string
		wantErr  error
	}{
		{"valid", "db.example.com", 5432, "appdb", nil},
		{"host surrounding whitespace is trimmed", "  db.example.com ", 5432, "appdb", nil},
		{"empty host", "", 5432, "appdb", connection.ErrInvalidTarget},
		{"whitespace-only host", "   ", 5432, "appdb", connection.ErrInvalidTarget},
		{"host with inner whitespace", "db example.com", 5432, "appdb", connection.ErrInvalidTarget},
		{"host with control char", "db\x00.example.com", 5432, "appdb", connection.ErrInvalidTarget},
		// URI-structural characters would make pgx dial a different target than the stored descriptor (comma → multi-host, slash/@/?/# → confusion).
		{"host with comma (multi-host)", "a,b", 5432, "appdb", connection.ErrInvalidTarget},
		{"host with slash", "h/x", 5432, "appdb", connection.ErrInvalidTarget},
		{"host with userinfo @", "u@h", 5432, "appdb", connection.ErrInvalidTarget},
		{"host with query ?", "h?x", 5432, "appdb", connection.ErrInvalidTarget},

		{"host with pipe (fingerprint separator)", "db|5432", 1234, "prod", connection.ErrInvalidTarget},
		{"ipv6 literal is allowed", "::1", 5432, "appdb", nil},
		{"canonical dotted-quad is allowed", "10.20.30.40", 5432, "appdb", nil},
		{"IPv4-mapped IPv6 literal is allowed", "::ffff:10.0.0.1", 5432, "appdb", nil},
		{"hostname with numeric inner labels is allowed", "10.db.example.com", 5432, "appdb", nil},
		{"hostname label starting with digits is allowed", "1password.example.com", 5432, "appdb", nil},
		// Non-canonical numeric spellings that resolvers may still read as IPv4 (inet_aton) are rejected so an address cannot hide behind them (ADR-0051).
		{"decimal IPv4 spelling", "2130706433", 5432, "appdb", connection.ErrInvalidTarget},
		{"octal IPv4 spelling", "0177.0.0.1", 5432, "appdb", connection.ErrInvalidTarget},
		{"hexadecimal IPv4 spelling", "0x7f000001", 5432, "appdb", connection.ErrInvalidTarget},
		{"mixed hexadecimal octet", "0x7f.0.0.1", 5432, "appdb", connection.ErrInvalidTarget},
		{"shortened IPv4 spelling", "127.1", 5432, "appdb", connection.ErrInvalidTarget},
		{"out-of-range octet", "10.0.0.256", 5432, "appdb", connection.ErrInvalidTarget},
		{"trailing-dot numeric spelling", "169.254.169.254.", 5432, "appdb", connection.ErrInvalidTarget},
		{"numeric top-level label", "db.123", 5432, "appdb", connection.ErrInvalidTarget},
		{"host over limit", strings.Repeat("h", 256), 5432, "appdb", connection.ErrInvalidTarget},
		{"port zero", "db.example.com", 0, "appdb", connection.ErrInvalidTarget},
		{"port negative", "db.example.com", -1, "appdb", connection.ErrInvalidTarget},
		{"port over 65535", "db.example.com", 65536, "appdb", connection.ErrInvalidTarget},
		{"empty database", "db.example.com", 5432, "", connection.ErrInvalidTarget},

		{"database with space is allowed", "db.example.com", 5432, "team database", nil},
		{"database with control char", "db.example.com", 5432, "app\ndb", connection.ErrInvalidTarget},
		{"database with NUL", "db.example.com", 5432, "app\x00db", connection.ErrInvalidTarget},
		{"database over limit", "db.example.com", 5432, strings.Repeat("d", 256), connection.ErrInvalidTarget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := connection.NewTarget(tt.host, tt.port, tt.database)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewTarget err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.Host != strings.TrimSpace(tt.host) {
				t.Errorf("Host = %q, want trimmed %q", got.Host, strings.TrimSpace(tt.host))
			}
		})
	}
}

func TestFingerprintPinsV1Layout(t *testing.T) {
	t.Parallel()
	target, err := connection.NewTarget("DB.Example.com", 5432, "appdb")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("portcullis/fingerprint/v1|postgresql|db.example.com|5432|appdb"))
	want := hex.EncodeToString(sum[:])
	if got := target.Fingerprint(connection.DBTypePostgreSQL); got != want {
		t.Errorf("Fingerprint = %q, want pinned v1 digest %q", got, want)
	}
}

func TestFingerprintProperties(t *testing.T) {
	t.Parallel()
	mk := func(host string, port int, db string) string {
		t.Helper()
		target, err := connection.NewTarget(host, port, db)
		if err != nil {
			t.Fatal(err)
		}
		return target.Fingerprint(connection.DBTypePostgreSQL)
	}

	base := mk("db.example.com", 5432, "appdb")
	if again := mk("db.example.com", 5432, "appdb"); again != base {
		t.Error("fingerprint is not deterministic")
	}
	// Host case must not change the identity of the target.
	if upper := mk("DB.EXAMPLE.COM", 5432, "appdb"); upper != base {
		t.Error("fingerprint should be case-insensitive on host")
	}
	if otherPort := mk("db.example.com", 5433, "appdb"); otherPort == base {
		t.Error("fingerprint should differ by port")
	}
	if otherDB := mk("db.example.com", 5432, "other"); otherDB == base {
		t.Error("fingerprint should differ by database")
	}

	if dbCase := mk("db.example.com", 5432, "AppDB"); dbCase == base {
		t.Error("fingerprint must not fold database name case")
	}
	if len(base) != sha256.Size*2 {
		t.Errorf("fingerprint length = %d, want %d hex chars", len(base), sha256.Size*2)
	}

	if _, err := connection.NewTarget("db|5432", 1234, "prod"); !errors.Is(err, connection.ErrInvalidTarget) {
		t.Errorf("pipe-bearing host must be rejected, got %v", err)
	}
	if fp := mk("db", 5432, "1234|prod"); fp == base {
		t.Error("pipe-bearing database should still fingerprint distinctly")
	}
}
