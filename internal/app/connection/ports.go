package connection

import (
	"context"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Repository is the narrow storage surface the connection use cases need —
// only the methods this service calls (ISP). The postgres ConnectionStore
// satisfies it, and tests use an in-memory fake. Mutations carry the audit
// events that commit in the same transaction as the change (ADR-0009).
type Repository interface {
	// DefaultOrganizationID resolves the single self-hosted organization all
	// queries are scoped to (single-org MVP, ADR-0004).
	DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error)

	Create(ctx context.Context, c connection.Connection, cred connection.SealedCredential, events ...audit.Event) error
	GetByID(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (connection.Connection, error)
	List(ctx context.Context, org identity.OrganizationID, includeArchived bool) ([]connection.Connection, error)
	// UpdateDescriptor changes the credential-free descriptor fields (name,
	// environment label, description) — archived rows included: they label
	// history, not the live target.
	UpdateDescriptor(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID, displayName string, env connection.Environment, description string, events ...audit.Event) (connection.Connection, error)
	// ReplaceConfig swaps display name, target, TLS mode, fingerprint, and the
	// sealed credential in one statement; archived rows fail with ErrArchived.
	// ReplaceConfig persists c only if the row still has expectedVersion. This
	// prevents a slow target test from overwriting a concurrent rename or config
	// change; a mismatch fails with connection.ErrConflict.
	ReplaceConfig(ctx context.Context, c connection.Connection, expectedVersion int64, cred connection.SealedCredential, events ...audit.Event) (connection.Connection, error)
	// Archive soft-deletes and nulls the credential columns atomically (PRD
	// §4.3); already-archived rows fail with ErrAlreadyArchived.
	Archive(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID, events ...audit.Event) (connection.Connection, error)
	// TestMaterial loads the descriptor and the sealed credential in a SINGLE
	// statement so a test-by-id can never mix an old target with a freshly
	// replaced credential (a config replace committing between two reads would,
	// under READ COMMITTED, dial the old host with the new credential —
	// ADR-0014). Archived rows fail with ErrArchived (credential discarded).
	TestMaterial(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (connection.Connection, connection.SealedCredential, error)
}

// Tester dials a target database and authenticates, within the adapter's
// configured timeout. It returns nil on success or a *TestError whose bucket
// is safe to show a caller — never raw driver text (PRD §8.1, ADR-0014). This
// port is the seam the future PG dialect adapter's ValidateConnection absorbs
// (PRD §5.3).
type Tester interface {
	Test(ctx context.Context, target connection.Target, mode connection.TLSMode, cred connection.Credential) error
}

// CredentialCodec seals a credential into the ADR-0003 envelope under the
// canonical AAD (record type connection_credential, the org, and the
// connection id) and opens it back. The application depends on this small,
// consumer-defined interface rather than the concrete crypto package;
// infra/crypto provides the keyring-backed adapter.
type CredentialCodec interface {
	Seal(org identity.OrganizationID, id connection.ConnectionID, cred connection.Credential) (connection.SealedCredential, error)
	Open(org identity.OrganizationID, id connection.ConnectionID, sealed connection.SealedCredential) (connection.Credential, error)
}

// AuditRecorder persists one audit event. Used only for the best-effort
// (detached) CONNECTION_TEST trail — state-changing events ride the Repository
// mutations instead (ADR-0009). A failure is logged, never surfaced.
type AuditRecorder interface {
	Record(ctx context.Context, e audit.Event) error
}
