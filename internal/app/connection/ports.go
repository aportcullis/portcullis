package connection

import (
	"context"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Repository mutations commit audit events atomically and observe expiry under the request lock before deciding (ADR-0009/0018).
type Repository interface {
	// DefaultOrganizationID resolves the single self-hosted organization all queries are scoped to (single-org MVP, ADR-0004).
	DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error)

	Create(ctx context.Context, c connection.Connection, cred connection.SealedCredential, events ...audit.Event) error
	GetByID(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (connection.Connection, error)
	List(ctx context.Context, org identity.OrganizationID, includeArchived bool) ([]connection.Connection, error)
	// UpdateDescriptor replaces descriptor fields, including archived history labels, only at expectedVersion; stale edits return ErrConflict.
	UpdateDescriptor(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID, displayName string, env connection.Environment, description string, expectedVersion int64, events ...audit.Event) (connection.Connection, error)
	// ReplaceConfig atomically replaces the target and credential at expectedVersion; archived targets and stale edits are refused.
	ReplaceConfig(ctx context.Context, c connection.Connection, expectedVersion int64, cred connection.SealedCredential, events ...audit.Event) (connection.Connection, error)
	// Archive soft-deletes and nulls the credential columns atomically (PRD §4.3); already-archived rows fail with ErrAlreadyArchived.
	Archive(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID, events ...audit.Event) (connection.Connection, error)
	// TestMaterial reads target and credential together to avoid mixing config versions; archived connections are refused.
	TestMaterial(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (connection.Connection, connection.SealedCredential, error)
}

// ConnectionValidator dials a target database and authenticates, within the adapter's configured timeout. It returns nil on success or a *TestError whose bucket is safe to show a caller — never raw driver text (PRD §8.1, ADR-0014). Implemented by the dialect adapter's ValidateConnection (PRD §5.3).
type ConnectionValidator interface {
	ValidateConnection(ctx context.Context, target connection.Target, mode connection.TLSMode, cred connection.Credential) error
}

// CredentialCodec seals a credential into the ADR-0003 envelope under the canonical AAD (record type connection_credential, the org, and the connection id) and opens it back. The application depends on this small, consumer-defined interface rather than the concrete crypto package; infra/crypto provides the keyring-backed adapter.
type CredentialCodec interface {
	Seal(org identity.OrganizationID, id connection.ConnectionID, cred connection.Credential) (connection.SealedCredential, error)
	Open(org identity.OrganizationID, id connection.ConnectionID, sealed connection.SealedCredential) (connection.Credential, error)
}

// AuditRecorder persists one audit event. Used only for the best-effort (detached) CONNECTION_TEST trail — state-changing events ride the Repository mutations instead (ADR-0009). A failure is logged, never surfaced.
type AuditRecorder interface {
	Record(ctx context.Context, e audit.Event) error
}
