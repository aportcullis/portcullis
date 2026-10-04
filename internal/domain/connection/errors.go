package connection

import "errors"

var (
	ErrNotFound = errors.New("connection: not found")

	ErrNameTaken = errors.New("connection: display name already in use")

	ErrAlreadyArchived = errors.New("connection: already archived")

	ErrArchived = errors.New("connection: connection is archived")

	ErrConflict = errors.New("connection: changed concurrently")

	ErrUnsupportedDBType = errors.New("connection: unsupported database type")

	ErrInvalidTLSMode = errors.New("connection: invalid tls mode")

	ErrInvalidTarget = errors.New("connection: invalid target")

	ErrInvalidDisplayName = errors.New("connection: invalid display name")

	ErrInvalidCredential = errors.New("connection: invalid credential")

	ErrInvalidConnection = errors.New("connection: invalid connection")

	ErrInvalidEnvironment = errors.New("connection: invalid environment")

	ErrInvalidDescription = errors.New("connection: invalid description")

	ErrInvalidPolicy = errors.New("connection: invalid policy")

	ErrPolicyConflict = errors.New("connection: policy changed concurrently")

	ErrExecutionInFlight = errors.New("connection: an execution is in flight")

	ErrInvalidDestinationPolicy = errors.New("connection: invalid destination policy")
)

// TestBucket is the coarse, caller-safe classification of a failed connection test (ADR-0014) — a fixed domain enum. Buckets are the only test-failure detail that crosses the API boundary: raw driver errors may embed credentials and never leave the dialect adapter (PRD §8.1).
type TestBucket string

// Test-failure buckets.
const (
	TestBucketUnreachable     TestBucket = "unreachable"
	TestBucketAuthFailed      TestBucket = "auth-failed"
	TestBucketTLSFailed       TestBucket = "tls-failed"
	TestBucketUnknownDatabase TestBucket = "unknown-database"
	TestBucketTimeout         TestBucket = "timeout"
	// TestBucketDestinationRefused covers every destination-policy refusal alike, so callers cannot learn which rule or resolved address refused the dial (ADR-0051).
	TestBucketDestinationRefused TestBucket = "destination-refused"
	// TestBucketFailed is the fallback for anything unclassified.
	TestBucketFailed TestBucket = "failed"
)

// TestError is a failed connection test. Its message is the bucket, nothing else, so it is safe to surface verbatim.
type TestError struct {
	Bucket TestBucket
}

func (e *TestError) Error() string {
	return "connection test failed: " + string(e.Bucket)
}
