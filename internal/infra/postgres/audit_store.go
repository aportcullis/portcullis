package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// AuditStore persists audit events into the append-only audit_events table. It
// satisfies auth.AuditRecorder structurally; mutation and truncation are blocked
// by DB triggers, so the adapter only ever inserts.
type AuditStore struct {
	q *db.Queries
}

// NewAuditStore builds the store on a connection pool.
func NewAuditStore(pool *pgxpool.Pool) *AuditStore {
	return &AuditStore{q: db.New(pool)}
}

// Record inserts one event (best-effort path — no surrounding transaction).
func (s *AuditStore) Record(ctx context.Context, e audit.Event) error {
	p, err := auditEventParams(e)
	if err != nil {
		return err
	}
	return s.q.InsertAuditEvent(ctx, p)
}

// insertAuditTx writes an event through the given (transaction-bound) queries,
// completing the organization when the caller left it empty (single-org MVP) —
// used by IdentityStore so the event commits atomically with its state change.
func insertAuditTx(ctx context.Context, q *db.Queries, evt audit.Event) error {
	if evt.OrganizationID == "" {
		org, err := q.GetDefaultOrganization(ctx)
		if err != nil {
			return err
		}
		evt.OrganizationID = identity.OrganizationID(uuidToString(org.ID))
	}
	p, err := auditEventParams(evt)
	if err != nil {
		return err
	}
	return q.InsertAuditEvent(ctx, p)
}

// auditEventParams maps a domain event onto the insert parameters. Shared by the
// best-effort store above and by IdentityStore's transactional writes (ADR-0009).
// The source IP travels in the metadata JSONB (the schema has no dedicated
// column); occurred_at is the insert-time DB default.
func auditEventParams(e audit.Event) (db.InsertAuditEventParams, error) {
	org, err := stringToUUID(string(e.OrganizationID))
	if err != nil {
		return db.InsertAuditEventParams{}, err
	}
	p := db.InsertAuditEventParams{
		OrganizationID: org,
		ActorType:      string(e.ActorType),
		Action:         string(e.Action),
		TargetType:     e.TargetType,
		Outcome:        string(e.Outcome),
	}
	if e.ActorUserID != nil {
		if p.ActorUserID, err = stringToUUID(string(*e.ActorUserID)); err != nil {
			return db.InsertAuditEventParams{}, err
		}
	}
	if e.ActorService != "" {
		p.ActorService = &e.ActorService
	}
	if e.TargetID != "" {
		p.TargetID = &e.TargetID
	}
	if e.RequestID != "" {
		p.RequestID = &e.RequestID
	}
	// Copy the metadata rather than mutating the caller's map.
	meta := make(map[string]any, len(e.Metadata)+1)
	for k, v := range e.Metadata {
		meta[k] = v
	}
	if e.SourceIP != "" {
		meta["source_ip"] = e.SourceIP
	}
	if p.Metadata, err = json.Marshal(meta); err != nil {
		return db.InsertAuditEventParams{}, err
	}
	return p, nil
}
