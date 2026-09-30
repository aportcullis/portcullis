package accessrequest

import (
	"context"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

// Repository mutations commit audit events atomically and observe expiry under the request lock before deciding (ADR-0009/0018).
type Repository interface {
	// DefaultOrganizationID resolves the single self-hosted organization (single-org MVP, ADR-0004).
	DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error)

	// CurrentTarget returns what a submit pins about its target: the current policy AND the connection's config version + fingerprint. It refuses archived connections (access.ErrConnectionArchived): an archived connection accepts no new requests (§4.3), unlike the policies service's own Get.
	CurrentTarget(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (access.SubmitTarget, error)

	// CreateDraft persists a new draft and its sealed payload, returning the complete read view (connection name, requester fields) fetched in the same transaction — so mutation responses are authoritative list rows.
	CreateDraft(ctx context.Context, r access.Request, sealed access.SealedPayload, events ...audit.Event) (access.RequestView, error)
	// GetSealed returns the bare row plus the sealed payload (edit/submit/ authorized-view paths). Org-scoped; access.ErrNotFound otherwise.
	GetSealed(ctx context.Context, org identity.OrganizationID, id access.RequestID) (access.Request, access.SealedPayload, error)
	// UpdateDraft replaces the payload of a draft, guarded by state='draft' AND version=expectedVersion (access.ErrNotDraft / access.ErrConflict), returning the complete view.
	UpdateDraft(ctx context.Context, r access.Request, sealed access.SealedPayload, expectedVersion int64, events ...audit.Event) (access.RequestView, error)
	// Submit atomically stores the immutable snapshot and guarded transition. Approval validity starts after the connection lock is acquired so lock waits do not consume it.
	Submit(ctx context.Context, r access.Request, expectedVersion int64, validity time.Duration, events ...audit.Event) (access.RequestView, error)

	// Approve revalidates eligibility under lock, appends a distinct decision, and transitions when valid approvals reach quorum. The store completes audit snapshot and expiry fields.
	Approve(ctx context.Context, org identity.OrganizationID, id access.RequestID, approver identity.UserID, reason string, validity time.Duration, evt audit.Event) (access.RequestView, error)
	// Reject records a rejection the same way; one rejection is terminal.
	Reject(ctx context.Context, org identity.OrganizationID, id access.RequestID, approver identity.UserID, reason string, evt audit.Event) (access.RequestView, error)
	// Cancel is the requester's own cancellation (draft/pending/approved), returning the complete view.
	Cancel(ctx context.Context, org identity.OrganizationID, id access.RequestID, requester identity.UserID, evt audit.Event) (access.RequestView, error)

	// Get returns the read view (requester/approver names, valid-approval count) plus the sealed payload — the detail query already selects the payload columns, so it rides the same row (no second round-trip). Org-scoped.
	Get(ctx context.Context, org identity.OrganizationID, id access.RequestID) (access.RequestView, access.SealedPayload, error)
	// List returns one §7.1 page; q.RequesterID (when set) scopes to one requester's rows.
	List(ctx context.Context, org identity.OrganizationID, q access.ListQuery) (access.RequestPage, error)
}

// RequestTargets lists active connections available to requesters.
type RequestTargets interface {
	ListRequestable(ctx context.Context, org identity.OrganizationID) ([]access.RequestableConnection, error)
}

// PayloadCodec encrypts, decrypts, and authenticates approval payloads.
type PayloadCodec interface {
	Seal(org identity.OrganizationID, id access.RequestID, p access.Payload) (access.SealedPayload, error)
	Open(org identity.OrganizationID, id access.RequestID, sealed access.SealedPayload) (access.Payload, error)
	// Digest returns the HMAC of the canonical approval-unit bytes plus the key version that produced it (ADR-0003; the pair is stored all-or-nothing). The service builds the canonical serialization (access.CanonicalPayload) so the digest binds the FULL approval unit, not just the SQL (§4.3).
	Digest(canonical []byte) ([]byte, uint32, error)
}

// Dialect binds, parses, classifies, and redacts submitted SQL.
type Dialect interface {
	ParseSingle(sql string) (query.Statement, error)
	Classify(st query.Statement) (query.StatementClass, error)
	BindNamed(sql string, params []query.Parameter) (string, []query.TypedValue, error)
	Redact(st query.Statement) (query.Redaction, error)
}
