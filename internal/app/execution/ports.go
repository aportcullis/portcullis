package execution

import (
	"context"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

// LeaseRepository atomically records execution ownership, transitions, and audit evidence.
type LeaseRepository interface {
	AcquireExecution(ctx context.Context, org identity.OrganizationID, requestID access.RequestID, requester identity.UserID, owner, attemptID string, digest []byte, event audit.Event) (access.ExecutionLease, error)
	HeartbeatExecution(ctx context.Context, org identity.OrganizationID, lease access.ExecutionLease) error
	CompleteExecution(ctx context.Context, org identity.OrganizationID, lease access.ExecutionLease, completion access.ExecutionCompletion, event audit.Event) error
	ReconcileExecutions(ctx context.Context, org identity.OrganizationID, after access.ReconcileCursor, batchSize int) (access.ReconcileBatch, error)
	GetExecution(context.Context, identity.OrganizationID, access.RequestID) (access.ExecutionCompletion, error)
	RecordExecutionRefusal(context.Context, identity.OrganizationID, access.RequestID, identity.UserID, string, audit.Event) error
}

// RequestRepository returns the encrypted, organization-scoped approval unit.
type RequestRepository interface {
	DefaultOrganizationID(context.Context) (identity.OrganizationID, error)
	GetSealed(context.Context, identity.OrganizationID, access.RequestID) (access.Request, access.SealedPayload, error)
	CurrentTarget(context.Context, identity.OrganizationID, connection.ConnectionID) (access.SubmitTarget, error)
}

// ConnectionRepository supplies the approved connection's sealed credential.
type ConnectionRepository interface {
	TestMaterial(context.Context, identity.OrganizationID, connection.ConnectionID) (connection.Connection, connection.SealedCredential, error)
}

// PayloadCodec opens and verifies the immutable approval payload.
type PayloadCodec interface {
	Open(identity.OrganizationID, access.RequestID, access.SealedPayload) (access.Payload, error)
	Verify([]byte, []byte, uint32) (bool, error)
}

// CredentialCodec opens the target credential for the dedicated executor.
type CredentialCodec interface {
	Open(identity.OrganizationID, connection.ConnectionID, connection.SealedCredential) (connection.Credential, error)
}

// Dialect binds and classifies the exact stored payload before execution.
type Dialect interface {
	ParseSingle(string) (query.Statement, error)
	Classify(query.Statement) (query.StatementClass, error)
	BindNamed(string, []query.Parameter) (string, []query.TypedValue, error)
	Execute(context.Context, connection.Target, connection.TLSMode, connection.Credential, query.Execution) (query.ResultStream, error)
}

// ResultWriter persists a bounded encrypted snapshot after confirmed execution.
type ResultWriter interface {
	Save(context.Context, query.SnapshotMetadata, []query.Column, [][]query.CellValue, int64) (query.SnapshotMetadata, error)
}

// Admission sheds unhealthy target load before a request consumes its lease.
type Admission interface {
	Allow(connection.ConnectionID) (func(TargetOutcome), error)
}

// TargetOutcome distinguishes target availability from unattempted or inconclusive executions.
type TargetOutcome uint8

// Target outcomes report availability without treating an unused reservation as success.
const (
	TargetNotAttempted TargetOutcome = iota
	TargetHealthy
	TargetUnhealthy
	TargetInconclusive
)

// DialectResolver selects the registered dialect for a stored target engine.
type DialectResolver interface {
	ExecutionDialect(connection.DBType) (Dialect, error)
}
