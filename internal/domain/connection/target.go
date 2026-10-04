package connection

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strings"
	"unicode"
)

// Byte-length bounds for target fields. Hostnames cap at 255 octets (RFC 1035); the same generous bound serves database names, which PostgreSQL truncates far earlier (63 bytes) but other engines and quoting rules vary.
const (
	maxHostLength         = 255
	maxDatabaseNameLength = 255
)

// fingerprintPrefixV1 versions the fingerprint derivation. The digest is stored plaintext and survives archive as the target-identifying snapshot (PRD §4.3), so changing the layout is a breaking change to history and must be a new version, never an edit.
const fingerprintPrefixV1 = "portcullis/fingerprint/v1"

// Target is where a connection points: the network coordinates a connection test dials and the fingerprint's input. It carries no credential.
type Target struct {
	Host         string
	Port         uint16
	DatabaseName string
}

// NewTarget validates and canonicalizes the target coordinates. The host is trimmed of surrounding whitespace (a paste artifact, not identity); casing is preserved for display and folded only in the fingerprint. All failures return ErrInvalidTarget.
func NewTarget(host string, port int, database string) (Target, error) {
	host = strings.TrimSpace(host)
	// Reject host delimiters, whitespace, and control characters so dialing and fingerprinting identify the same single target (ADR-0014).
	if host == "" || len(host) > maxHostLength || containsSpaceOrControl(host) || strings.ContainsAny(host, ",/@?#|") {
		return Target{}, ErrInvalidTarget
	}
	if isNonCanonicalNumericHost(host) {
		return Target{}, ErrInvalidTarget
	}
	if port < 1 || port > 65535 {
		return Target{}, ErrInvalidTarget
	}
	// A PostgreSQL quoted identifier — and so a database name — may contain any character except the null (web-verified: PG lexical structure). The name is carried in the pgconn Config structurally, not spliced into a DSN, so spaces ("team database") are safe; only control characters (incl. NUL, which would truncate) are rejected.
	if database == "" || len(database) > maxDatabaseNameLength || containsControl(database) {
		return Target{}, ErrInvalidTarget
	}
	return Target{Host: host, Port: uint16(port), DatabaseName: database}, nil
}

// Fingerprint derives the versioned target identity (ADR-0014): hex(sha256("portcullis/fingerprint/v1|<db_type>|<lower(host)>|<port>|<database>")). The host folds because DNS is case-insensitive; the database name does not — it is case-sensitive in PostgreSQL.
func (t Target) Fingerprint(dbType DBType) string {
	input := fmt.Sprintf("%s|%s|%s|%d|%s",
		fingerprintPrefixV1, dbType, strings.ToLower(t.Host), t.Port, t.DatabaseName)
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

// isNonCanonicalNumericHost reports a host that is not a canonical IP literal but ends in a numeric label, which no DNS hostname does and which inet_aton-style resolvers read as decimal, octal or hexadecimal IPv4 (ADR-0051).
func isNonCanonicalNumericHost(host string) bool {
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	labels := strings.Split(strings.TrimSuffix(host, "."), ".")
	return isNumericLabel(labels[len(labels)-1])
}

// isNumericLabel reports a label made only of decimal digits or a 0x-prefixed hexadecimal number.
func isNumericLabel(label string) bool {
	digits, isHex := strings.CutPrefix(strings.ToLower(label), "0x")
	if digits == "" {
		return isHex
	}
	return !strings.ContainsFunc(digits, func(character rune) bool {
		if isHex {
			return !strings.ContainsRune("0123456789abcdef", character)
		}
		return character < '0' || character > '9'
	})
}

// containsSpaceOrControl rejects characters that can smuggle extra connection parameters or corrupt logs: any Unicode whitespace or control character. Used for the host, where whitespace is never legitimate.
func containsSpaceOrControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	})
}

// containsControl rejects only control characters (including NUL). Used for the database name, a PostgreSQL quoted identifier that legitimately allows spaces.
func containsControl(s string) bool {
	return strings.ContainsFunc(s, unicode.IsControl)
}
