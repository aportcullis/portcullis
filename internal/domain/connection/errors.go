package connection

import "errors"

var (
	// ErrNotFound means no connection matched the lookup within the caller's
	// organization scope.
	ErrNotFound = errors.New("connection: not found")
	// ErrNameTaken means an active connection with the same (case-folded) display
	// name already exists in the organization — the partial-unique conflict mapped
	// to a domain sentinel. Names free up when a connection is archived.
	ErrNameTaken = errors.New("connection: display name already in use")
	// ErrAlreadyArchived means Archive was called on an already-archived
	// connection; the original archive timestamp is evidence and must not move.
	ErrAlreadyArchived = errors.New("connection: already archived")
	// ErrArchived means the operation (test, config update, execution) is not
	// available on an archived connection — its credential was discarded (PRD
	// §4.3); restore requires credential re-entry.
	ErrArchived = errors.New("connection: connection is archived")
	// ErrConflict means another mutation changed an active connection after a
	// config update read it but before it could commit. The caller must reload
	// the descriptor and explicitly retry rather than overwrite that change.
	ErrConflict = errors.New("connection: changed concurrently")
	// ErrUnsupportedDBType means the database type has no dialect adapter yet
	// (M1 ships PostgreSQL only; MySQL/SQLite land in M2).
	ErrUnsupportedDBType = errors.New("connection: unsupported database type")
	// ErrInvalidTLSMode means the TLS mode is not in the accepted set — including
	// libpq's prefer/allow, which silently downgrade to plaintext (ADR-0014).
	ErrInvalidTLSMode = errors.New("connection: invalid tls mode")
	// ErrInvalidTarget means host, port, or database name failed validation.
	ErrInvalidTarget = errors.New("connection: invalid target")
	// ErrInvalidDisplayName means the display name is empty, over the length
	// limit, or contains control/format/separator characters.
	ErrInvalidDisplayName = errors.New("connection: invalid display name")
	// ErrInvalidCredential means the database user/password pair failed
	// structural validation (not an authentication failure at the target).
	ErrInvalidCredential = errors.New("connection: invalid credential")
	// ErrInvalidConnection means a required identity field (id, organization,
	// creator) was missing at construction.
	ErrInvalidConnection = errors.New("connection: invalid connection")
)

// TestBucket is the coarse, caller-safe classification of a failed connection
// test (ADR-0014) — a fixed domain enum. Buckets are the only test-failure
// detail that crosses the API boundary: raw driver errors may embed
// credentials and never leave the dialect adapter (PRD §8.1).
type TestBucket string

// Test-failure buckets.
const (
	TestBucketUnreachable     TestBucket = "unreachable"
	TestBucketAuthFailed      TestBucket = "auth-failed"
	TestBucketTLSFailed       TestBucket = "tls-failed"
	TestBucketUnknownDatabase TestBucket = "unknown-database"
	TestBucketTimeout         TestBucket = "timeout"
	// TestBucketFailed is the fallback for anything unclassified.
	TestBucketFailed TestBucket = "failed"
)

// TestError is a failed connection test. Its message is the bucket, nothing
// else, so it is safe to surface verbatim.
type TestError struct {
	Bucket TestBucket
}

func (e *TestError) Error() string {
	return "connection test failed: " + string(e.Bucket)
}
