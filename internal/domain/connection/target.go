package connection

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
)

// Byte-length bounds for target fields. Hostnames cap at 255 octets (RFC
// 1035); the same generous bound serves database names, which PostgreSQL
// truncates far earlier (63 bytes) but other engines and quoting rules vary.
const (
	maxHostLength         = 255
	maxDatabaseNameLength = 255
)

// fingerprintPrefixV1 versions the fingerprint derivation. The digest is
// stored plaintext and survives archive as the target-identifying snapshot
// (PRD §4.3), so changing the layout is a breaking change to history and must
// be a new version, never an edit.
const fingerprintPrefixV1 = "portcullis/fingerprint/v1"

// Target is where a connection points: the network coordinates a connection
// test dials and the fingerprint's input. It carries no credential.
type Target struct {
	Host         string
	Port         uint16
	DatabaseName string
}

// NewTarget validates and canonicalizes the target coordinates. The host is
// trimmed of surrounding whitespace (a paste artifact, not identity); casing
// is preserved for display and folded only in the fingerprint. All failures
// return ErrInvalidTarget.
func NewTarget(host string, port int, database string) (Target, error) {
	host = strings.TrimSpace(host)
	// A host is a single DNS name or IP literal. Reject whitespace/control, the
	// URI-structural characters (comma → pgx multi-host; slash/@/?/# →
	// authority/path/query confusion) so the stored descriptor is exactly the
	// one dialed and fingerprinted, and the fingerprint's | separator: with a
	// |-free host the v1 input parses uniquely (the port between host and
	// database is digits-only), so no two targets can collide even though the
	// database name may legitimately contain | (ADR-0014).
	if host == "" || len(host) > maxHostLength || containsSpaceOrControl(host) || strings.ContainsAny(host, ",/@?#|") {
		return Target{}, ErrInvalidTarget
	}
	if port < 1 || port > 65535 {
		return Target{}, ErrInvalidTarget
	}
	// A PostgreSQL quoted identifier — and so a database name — may contain any
	// character except the null (web-verified: PG lexical structure). The name
	// is carried in the pgconn Config structurally, not spliced into a DSN, so
	// spaces ("team database") are safe; only control characters (incl. NUL,
	// which would truncate) are rejected.
	if database == "" || len(database) > maxDatabaseNameLength || containsControl(database) {
		return Target{}, ErrInvalidTarget
	}
	return Target{Host: host, Port: uint16(port), DatabaseName: database}, nil
}

// Fingerprint derives the versioned target identity (ADR-0014):
// hex(sha256("portcullis/fingerprint/v1|<db_type>|<lower(host)>|<port>|<database>")).
// The host folds because DNS is case-insensitive; the database name does not —
// it is case-sensitive in PostgreSQL.
func (t Target) Fingerprint(dbType DBType) string {
	input := fmt.Sprintf("%s|%s|%s|%d|%s",
		fingerprintPrefixV1, dbType, strings.ToLower(t.Host), t.Port, t.DatabaseName)
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

// containsSpaceOrControl rejects characters that can smuggle extra connection
// parameters or corrupt logs: any Unicode whitespace or control character. Used
// for the host, where whitespace is never legitimate.
func containsSpaceOrControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	})
}

// containsControl rejects only control characters (including NUL). Used for the
// database name, a PostgreSQL quoted identifier that legitimately allows spaces.
func containsControl(s string) bool {
	return strings.ContainsFunc(s, unicode.IsControl)
}
