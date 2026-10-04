package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// AuditStore persists audit events into the append-only audit_events table. It satisfies auth.AuditRecorder structurally; mutation and truncation are blocked by DB triggers, so the adapter only ever inserts. The pool backs the one-snapshot list read (readSnapshot).
type AuditStore struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// NewAuditStore builds the store on a connection pool.
func NewAuditStore(pool *pgxpool.Pool) *AuditStore {
	return &AuditStore{pool: pool, q: db.New(pool)}
}

// Record inserts one event (best-effort path — no surrounding transaction) under the same explicit-organization contract as the transactional path, so the two cannot drift.
func (s *AuditStore) Record(ctx context.Context, e audit.Event) error {
	return insertAuditTx(ctx, s.q, e)
}

// DefaultOrganizationID resolves the single self-hosted organization the audit reader scopes to.
func (s *AuditStore) DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error) {
	org, err := s.q.GetDefaultOrganization(ctx)
	if err != nil {
		return "", err
	}
	return identity.OrganizationID(uuidToString(org.ID)), nil
}

// List reads one organization's page, total, and page clamp from one repository snapshot (PRD §7.1, ADR-0004).
func (s *AuditStore) List(ctx context.Context, orgID identity.OrganizationID, p audit.ListParams) (audit.EventPage, error) {
	organizationUUID, err := parseAuditOrganization(orgID)
	if err != nil {
		return audit.EventPage{}, err
	}
	page := p.Page
	var rows []auditListRow
	var total int64
	err = readSnapshot(ctx, s.pool, s.q, func(_ pgx.Tx, q *db.Queries) error {
		var err error
		// The two queries differ only in ORDER BY direction; their row types are structurally identical, so both convert to auditListRow for one mapper.
		limit := int64(p.PageSize)
		read := func(pageNumber int) error {
			offset := int64(pageNumber-1) * limit
			if p.SortDescending {
				got, err := q.ListAuditEventsDesc(ctx, db.ListAuditEventsDescParams{OrganizationID: organizationUUID, PageLimit: limit, RowOffset: offset})
				if err != nil {
					return err
				}
				rows = make([]auditListRow, len(got))
				for idx, r := range got {
					rows[idx] = auditListRow(r)
				}
				return nil
			}
			got, err := q.ListAuditEventsAsc(ctx, db.ListAuditEventsAscParams{OrganizationID: organizationUUID, PageLimit: limit, RowOffset: offset})
			if err != nil {
				return err
			}
			rows = make([]auditListRow, len(got))
			for idx, r := range got {
				rows[idx] = auditListRow(r)
			}
			return nil
		}

		if err := read(page); err != nil {
			return err
		}
		if len(rows) > 0 {
			total = rows[0].TotalCount
			return nil
		}
		if total, err = q.CountAuditEvents(ctx, organizationUUID); err != nil {
			return err
		}
		// Empty because the caller asked past the end: land on the last page that has rows. An empty TRAIL stays on page 1 — there is no page to go back to.
		last := int(total+int64(p.PageSize)-1) / p.PageSize
		if last == 0 {
			page = 1
			return nil
		}
		if page <= last {
			return nil // genuinely empty in range
		}
		page = last
		return read(page)
	})
	if err != nil {
		return audit.EventPage{}, err
	}

	events := make([]audit.Event, 0, len(rows))
	for _, r := range rows {
		e, err := auditEventFromRow(r, orgID)
		if err != nil {
			return audit.EventPage{}, err
		}
		events = append(events, e)
	}
	// Page is the page actually READ, which is what the caller must echo: after a clamp it is no longer the page that was asked for.
	return audit.EventPage{Events: events, Page: page, PageSize: p.PageSize, TotalCount: total}, nil
}

// Get returns one full audit event inside the caller's organization; a foreign id is not found. The detail endpoint is separately guarded by audit.get in the transport layer.
func (s *AuditStore) Get(ctx context.Context, orgID identity.OrganizationID, id string) (audit.Event, error) {
	organizationUUID, err := parseAuditOrganization(orgID)
	if err != nil {
		return audit.Event{}, err
	}
	eventID, err := stringToUUID(id)
	if err != nil {
		return audit.Event{}, err
	}
	row, err := s.q.GetAuditEvent(ctx, db.GetAuditEventParams{ID: eventID, OrganizationID: organizationUUID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return audit.Event{}, audit.ErrEventNotFound
		}
		return audit.Event{}, err
	}
	return auditEventFromRow(auditListRow(row), orgID)
}

// parseAuditOrganization refuses an empty organization scope and parses the rest.
func parseAuditOrganization(orgID identity.OrganizationID) (pgtype.UUID, error) {
	if orgID == "" {
		return pgtype.UUID{}, audit.ErrOrganizationRequired
	}
	return stringToUUID(string(orgID))
}

// auditListRow is the shared shape of the (structurally identical) Desc/Asc list rows, so one mapper serves both sort directions. If the selected columns change, sqlc regenerates both row types and this conversion fails to compile until they are realigned — a compile-time guard, not silent drift.
type auditListRow struct {
	ID                      pgtype.UUID
	OccurredAt              pgtype.Timestamptz
	ActorType               string
	ActorUserID             pgtype.UUID
	ActorService            *string
	Action                  string
	TargetType              string
	TargetID                *string
	Outcome                 string
	RequestID               *string
	PreviousState           *string
	NextState               *string
	ConnectionID            pgtype.UUID
	QueryType               *string
	PayloadDigest           []byte
	PayloadDigestKeyVersion *int32
	Metadata                []byte
	RowsAffected            *int64
	DurationMs              *int64
	// TotalCount is the page's total, computed by the SAME query as the rows (count(*) over ()) so the two cannot come from different snapshots. On the detail path the expression is literally 1 and the field goes unused.
	TotalCount int64
}

// auditEventFromRow maps a read row back onto the domain event — the inverse of auditEventParams. The source IP is recovered from the metadata JSONB and removed from the remaining supplemental map, so callers see the same SourceIP/Metadata split they wrote.
func auditEventFromRow(r auditListRow, org identity.OrganizationID) (audit.Event, error) {
	e := audit.Event{
		ID:                   uuidToString(r.ID),
		OccurredAt:           tsToTime(r.OccurredAt),
		OrganizationID:       org,
		ActorType:            audit.ActorType(r.ActorType),
		Action:               audit.Action(r.Action),
		TargetType:           r.TargetType,
		Outcome:              audit.Outcome(r.Outcome),
		RowsAffected:         r.RowsAffected,
		DurationMilliseconds: r.DurationMs,
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
	if r.PreviousState != nil {
		e.PreviousState = *r.PreviousState
	}
	if r.NextState != nil {
		e.NextState = *r.NextState
	}
	if id := uuidToString(r.ConnectionID); id != "" {
		e.ConnectionID = id
	}
	if r.QueryType != nil {
		e.QueryType = *r.QueryType
	}
	e.PayloadDigest = r.PayloadDigest
	if r.PayloadDigestKeyVersion != nil {
		e.PayloadDigestKeyVersion = uint32(*r.PayloadDigestKeyVersion)
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

// insertAuditTx writes an event through the given (transaction-bound) queries so it commits with its state change; an event without an organization is refused rather than attributed to the default one (ADR-0004).
func insertAuditTx(ctx context.Context, q *db.Queries, evt audit.Event) error {
	if evt.OrganizationID == "" {
		return audit.ErrOrganizationRequired
	}
	p, err := auditEventParams(evt)
	if err != nil {
		return err
	}
	return q.InsertAuditEvent(ctx, p)
}

// insertAuditBatch validates every event first, then appends them in one pipelined batch on the transaction-bound queries; any failure fails the caller's transaction.
func insertAuditBatch(ctx context.Context, q *db.Queries, events []audit.Event) error {
	if len(events) == 0 {
		return nil
	}
	params := make([]db.InsertAuditEventsParams, 0, len(events))
	for _, evt := range events {
		if evt.OrganizationID == "" {
			return audit.ErrOrganizationRequired
		}
		eventParams, err := auditEventParams(evt)
		if err != nil {
			return err
		}
		params = append(params, db.InsertAuditEventsParams(eventParams))
	}
	var batchErr error
	results := q.InsertAuditEvents(ctx, params)
	results.Exec(func(_ int, err error) {
		if err != nil && batchErr == nil {
			batchErr = err
		}
	})
	if err := results.Close(); err != nil && batchErr == nil {
		batchErr = err
	}
	return batchErr
}

// auditEventParams maps a domain event onto the insert parameters. Shared by the best-effort store above and by IdentityStore's transactional writes (ADR-0009). The source IP travels in the metadata JSONB (the schema has no dedicated column); occurred_at is the insert-time DB default.
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
		RowsAffected:   e.RowsAffected,
		DurationMs:     e.DurationMilliseconds,
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
	// occurred_at is normally the column DEFAULT — the DB's clock, out of the caller's hands. A DERIVED event may instead carry the DB instant that produced the observation it reports, so the event cannot predate it (ADR-0009; see observeOverdue).
	if !e.OccurredAt.IsZero() {
		p.OccurredAt = timeToTS(e.OccurredAt)
	}
	if e.RequestID != "" {
		p.RequestID = &e.RequestID
	}
	if e.PreviousState != "" {
		p.PreviousState = &e.PreviousState
	}
	if e.NextState != "" {
		p.NextState = &e.NextState
	}
	if e.ConnectionID != "" {
		if p.ConnectionID, err = stringToUUID(e.ConnectionID); err != nil {
			return db.InsertAuditEventParams{}, err
		}
	}
	if e.QueryType != "" {
		p.QueryType = &e.QueryType
	}
	// The digest pair is all-or-nothing (audit_digest_pairing CHECK): a digest without its key version is unverifiable, a version without a digest is meaningless. A half-set pair is a CALLER DEFECT, so refuse it — dropping both to NULL would satisfy the CHECK and lose the evidence silently.
	switch hasDigest, hasVersion := len(e.PayloadDigest) > 0, e.PayloadDigestKeyVersion > 0; {
	case hasDigest != hasVersion:
		return db.InsertAuditEventParams{}, fmt.Errorf(
			"audit: payload digest and key version must be set together (digest present: %t, key version present: %t)", hasDigest, hasVersion)
	case hasDigest:
		if e.PayloadDigestKeyVersion > math.MaxInt32 {
			return db.InsertAuditEventParams{}, fmt.Errorf("audit: payload digest key version %d exceeds the column's range", e.PayloadDigestKeyVersion)
		}
		p.PayloadDigest = e.PayloadDigest
		kv := int32(e.PayloadDigestKeyVersion) //nolint:gosec // bounded by the check above
		p.PayloadDigestKeyVersion = &kv
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
