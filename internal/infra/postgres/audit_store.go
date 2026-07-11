package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
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

// List returns one page of audit events, org-scoped to the single organization
// (single-org MVP), ordered by occurred_at in the requested direction with an id
// tie-breaker. The source IP is lifted back out of the metadata JSONB — where
// Record folds it in, the schema having no dedicated column — so the read shape
// mirrors the write shape. Limit/Offset are int64, so a large page offset never
// wraps negative.
func (s *AuditStore) List(ctx context.Context, p audit.ListParams) ([]audit.Event, error) {
	org, err := s.q.GetDefaultOrganization(ctx)
	if err != nil {
		return nil, err
	}
	orgID := identity.OrganizationID(uuidToString(org.ID))

	// The two queries differ only in ORDER BY direction; their row types are
	// structurally identical, so both convert to auditListRow for one mapper.
	var rows []auditListRow
	if p.SortDescending {
		got, err := s.q.ListAuditEventsDesc(ctx, db.ListAuditEventsDescParams{OrganizationID: org.ID, PageLimit: p.Limit, RowOffset: p.Offset})
		if err != nil {
			return nil, err
		}
		rows = make([]auditListRow, len(got))
		for i, r := range got {
			rows[i] = auditListRow(r)
		}
	} else {
		got, err := s.q.ListAuditEventsAsc(ctx, db.ListAuditEventsAscParams{OrganizationID: org.ID, PageLimit: p.Limit, RowOffset: p.Offset})
		if err != nil {
			return nil, err
		}
		rows = make([]auditListRow, len(got))
		for i, r := range got {
			rows[i] = auditListRow(r)
		}
	}

	events := make([]audit.Event, 0, len(rows))
	for _, r := range rows {
		e, err := auditEventFromRow(r, orgID)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

// Count returns the total number of audit events for the single organization —
// the denominator for the page controls (PRD §7.1).
func (s *AuditStore) Count(ctx context.Context) (int64, error) {
	org, err := s.q.GetDefaultOrganization(ctx)
	if err != nil {
		return 0, err
	}
	return s.q.CountAuditEvents(ctx, org.ID)
}

// auditListRow is the shared shape of the (structurally identical) Desc/Asc list
// rows, so one mapper serves both sort directions. If the selected columns change,
// sqlc regenerates both row types and this conversion fails to compile until they
// are realigned — a compile-time guard, not silent drift.
type auditListRow struct {
	ID           pgtype.UUID
	OccurredAt   pgtype.Timestamptz
	ActorType    string
	ActorUserID  pgtype.UUID
	ActorService *string
	Action       string
	TargetType   string
	TargetID     *string
	Outcome      string
	RequestID    *string
	Metadata     []byte
}

// auditEventFromRow maps a read row back onto the domain event — the inverse of
// auditEventParams. The source IP is recovered from the metadata JSONB and removed
// from the remaining supplemental map, so callers see the same SourceIP/Metadata
// split they wrote.
func auditEventFromRow(r auditListRow, org identity.OrganizationID) (audit.Event, error) {
	e := audit.Event{
		ID:             uuidToString(r.ID),
		OccurredAt:     tsToTime(r.OccurredAt),
		OrganizationID: org,
		ActorType:      audit.ActorType(r.ActorType),
		Action:         audit.Action(r.Action),
		TargetType:     r.TargetType,
		Outcome:        audit.Outcome(r.Outcome),
	}
	if id := uuidToString(r.ActorUserID); id != "" {
		uid := identity.UserID(id)
		e.ActorUserID = &uid
	}
	if r.ActorService != nil {
		e.ActorService = *r.ActorService
	}
	if r.TargetID != nil {
		e.TargetID = *r.TargetID
	}
	if r.RequestID != nil {
		e.RequestID = *r.RequestID
	}
	if len(r.Metadata) > 0 {
		var meta map[string]any
		if err := json.Unmarshal(r.Metadata, &meta); err != nil {
			return audit.Event{}, err
		}
		if ip, ok := meta["source_ip"].(string); ok {
			e.SourceIP = ip
			delete(meta, "source_ip")
		}
		if len(meta) > 0 {
			e.Metadata = meta
		}
	}
	return e, nil
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
