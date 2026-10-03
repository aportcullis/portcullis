package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// AccessRequestStore persists requests, approvals, and audit events atomically.
type AccessRequestStore struct {
	// conns is reused for org resolution, the withTx helper, and the shared converters — same pool, same queries.
	conns *ConnectionStore
}

// NewAccessRequestStore builds the store on a connection pool.
func NewAccessRequestStore(pool *pgxpool.Pool) *AccessRequestStore {
	return &AccessRequestStore{conns: NewConnectionStore(pool)}
}

// DefaultOrganizationID resolves the single self-hosted organization.
func (s *AccessRequestStore) DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error) {
	return s.conns.DefaultOrganizationID(ctx)
}

// lockConnection uses FOR SHARE so concurrent request writers proceed while archive and policy changes wait for their commit (ADR-0018).
func (s *AccessRequestStore) lockConnection(ctx context.Context, q *db.Queries, cid, organizationUUID pgtype.UUID) (db.LockConnectionForRequestRow, error) {
	row, err := q.LockConnectionForRequest(ctx, db.LockConnectionForRequestParams{ID: cid, OrganizationID: organizationUUID})
	if err != nil {
		return db.LockConnectionForRequestRow{}, notFound(err, connection.ErrNotFound)
	}
	if row.ArchivedAt.Valid {
		return db.LockConnectionForRequestRow{}, access.ErrConnectionArchived
	}
	return row, nil
}

// ListRequestable returns active connection summaries for request forms.
func (s *AccessRequestStore) ListRequestable(ctx context.Context, org identity.OrganizationID) ([]access.RequestableConnection, error) {
	organizationUUID, err := stringToUUID(string(org))
	if err != nil {
		return nil, err
	}
	rows, err := s.conns.q.ListRequestableConnections(ctx, organizationUUID)
	if err != nil {
		return nil, err
	}
	out := make([]access.RequestableConnection, 0, len(rows))
	for _, r := range rows {
		out = append(out, access.RequestableConnection{
			ID:          connection.ConnectionID(uuidToString(r.ID)),
			DisplayName: r.DisplayName,
			DBType:      r.DbType,
			Environment: r.Environment,
		})
	}
	return out, nil
}

// CurrentTarget returns the active target configuration and policy snapshot.
func (s *AccessRequestStore) CurrentTarget(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (access.SubmitTarget, error) {
	cid, organizationUUID, err := connIDs(id, org)
	if err != nil {
		return access.SubmitTarget{}, err
	}
	conn, err := s.conns.q.GetConnection(ctx, db.GetConnectionParams{ID: cid, OrganizationID: organizationUUID})
	if err != nil {
		return access.SubmitTarget{}, notFound(err, connection.ErrNotFound)
	}
	if conn.ArchivedAt.Valid {
		return access.SubmitTarget{}, access.ErrConnectionArchived
	}
	row, err := s.conns.q.GetCurrentConnectionPolicy(ctx, db.GetCurrentConnectionPolicyParams{ID: cid, OrganizationID: organizationUUID})
	if err != nil {
		return access.SubmitTarget{}, notFound(err, connection.ErrNotFound)
	}
	// This read is a pre-lock pin, exactly like the policy version: Submit re-verifies both against the locked row (ADR-0018).
	return access.SubmitTarget{
		Policy:        toPolicy(row),
		ConfigVersion: conn.ConfigVersion,
		Fingerprint:   conn.TargetFingerprint,
		DisplayName:   conn.DisplayName,
		DBType:        conn.DbType,
	}, nil
}

// CreateDraft returns the full view from the same transaction as payload and audit writes.
func (s *AccessRequestStore) CreateDraft(ctx context.Context, r access.Request, sealed access.SealedPayload, events ...audit.Event) (access.RequestView, error) {
	requestUUID, organizationUUID, err := parseRequestAndOrganizationUUIDs(r.ID, r.OrganizationID)
	if err != nil {
		return access.RequestView{}, err
	}
	cid, err := stringToUUID(string(r.ConnectionID))
	if err != nil {
		return access.RequestView{}, fmt.Errorf("connection id: %w", err)
	}
	uid, err := stringToUUID(string(r.RequesterID))
	if err != nil {
		return access.RequestView{}, fmt.Errorf("requester id: %w", err)
	}
	var view access.RequestView
	err = s.conns.withTx(ctx, func(q *db.Queries) error {
		// Lock the connection and check its archive state INSIDE this transaction: an archive committing between any pre-read and this insert has already run its cascade sweep, so the draft would survive on an archived connection (§4.3, ADR-0018).
		if _, err := s.lockConnection(ctx, q, cid, organizationUUID); err != nil {
			return err
		}
		// The lock above may have parked behind an archive or a policy bump, so the row and its event are dated from an instant read AFTER it, never from now() (ADR-0009).
		at, err := observeInstant(ctx, q)
		if err != nil {
			return err
		}
		if err := q.InsertAccessRequest(ctx, db.InsertAccessRequestParams{
			ID:                requestUUID,
			OrganizationID:    organizationUUID,
			ConnectionID:      cid,
			RequesterID:       uid,
			Title:             r.Title,
			PayloadKeyVersion: int32(sealed.KeyVersion), //nolint:gosec // key versions count rotations, far below int32
			PayloadWrappedDek: sealed.WrappedDEK,
			PayloadNonce:      sealed.Nonce,
			PayloadCiphertext: sealed.Ciphertext,
			At:                timeToTS(at),
		}); err != nil {
			return err
		}
		if err := insertEvents(ctx, q, stampEvents(events, at)); err != nil {
			return err
		}
		view, err = s.loadRequestViewInTransaction(ctx, q, requestUUID, organizationUUID, uid)
		return err
	})
	if err != nil {
		return access.RequestView{}, err
	}
	return view, nil
}

// GetSealed returns the bare row plus its sealed payload, org-scoped.
func (s *AccessRequestStore) GetSealed(ctx context.Context, org identity.OrganizationID, id access.RequestID) (access.Request, access.SealedPayload, error) {
	requestUUID, organizationUUID, err := parseRequestAndOrganizationUUIDs(id, org)
	if err != nil {
		return access.Request{}, access.SealedPayload{}, err
	}
	row, err := s.conns.q.GetAccessRequest(ctx, db.GetAccessRequestParams{ID: requestUUID, OrganizationID: organizationUUID})
	if err != nil {
		return access.Request{}, access.SealedPayload{}, notFound(err, access.ErrNotFound)
	}
	return toAccessRequest(row), toSealedPayload(row), nil
}

// UpdateDraft replaces a draft payload after checking state and version.
func (s *AccessRequestStore) UpdateDraft(ctx context.Context, r access.Request, sealed access.SealedPayload, expectedVersion int64, events ...audit.Event) (access.RequestView, error) {
	requestUUID, organizationUUID, err := parseRequestAndOrganizationUUIDs(r.ID, r.OrganizationID)
	if err != nil {
		return access.RequestView{}, err
	}
	var view access.RequestView
	err = s.conns.withTx(ctx, func(q *db.Queries) error {
		// Take the row lock as its own statement before writing. The UPDATE would acquire it anyway, but then the wait would sit INSIDE the statement that stamps the row, and a stamp cannot describe a wait it is part of (ADR-0009). Locking first also means the instant below is read with the row already ours.
		if _, err := q.GetAccessRequestForUpdate(ctx, db.GetAccessRequestForUpdateParams{ID: requestUUID, OrganizationID: organizationUUID}); err != nil {
			return notFound(err, access.ErrNotFound)
		}
		at, err := observeInstant(ctx, q)
		if err != nil {
			return err
		}
		if _, err := q.UpdateAccessRequestDraftPayload(ctx, db.UpdateAccessRequestDraftPayloadParams{
			ID:                requestUUID,
			OrganizationID:    organizationUUID,
			ExpectedVersion:   expectedVersion,
			Title:             r.Title,
			PayloadKeyVersion: int32(sealed.KeyVersion), //nolint:gosec // key versions count rotations, far below int32
			PayloadWrappedDek: sealed.WrappedDEK,
			PayloadNonce:      sealed.Nonce,
			PayloadCiphertext: sealed.Ciphertext,
			At:                timeToTS(at),
		}); err != nil {
			return s.draftGuardError(ctx, q, requestUUID, organizationUUID, err)
		}
		if err := insertEvents(ctx, q, stampEvents(events, at)); err != nil {
			return err
		}
		view, err = s.loadRequestViewInTransaction(ctx, q, requestUUID, organizationUUID, pgtype.UUID{})
		return err
	})
	if err != nil {
		return access.RequestView{}, err
	}
	return view, nil
}

// Submit atomically stores the approval snapshot and submission transition.
func (s *AccessRequestStore) Submit(ctx context.Context, r access.Request, expectedVersion int64, validity time.Duration, events ...audit.Event) (access.RequestView, error) {
	requestUUID, organizationUUID, err := parseRequestAndOrganizationUUIDs(r.ID, r.OrganizationID)
	if err != nil {
		return access.RequestView{}, err
	}
	if r.SubmittedAt == nil {
		return access.RequestView{}, access.ErrInvalidRequest
	}
	kv := int32(r.DigestKeyVersion) //nolint:gosec // key versions count rotations, far below int32
	params := db.SubmitAccessRequestParams{
		ID:                      requestUUID,
		OrganizationID:          organizationUUID,
		ExpectedVersion:         expectedVersion,
		State:                   string(r.State),
		StatementClass:          ptr(string(r.Class)),
		PolicyVersion:           ptr(r.PolicyVersion),
		ConnectionConfigVersion: ptr(r.ConnectionConfigVersion),
		ConnectionFingerprint:   ptr(r.ConnectionFingerprint),
		ConnectionDisplayName:   ptr(r.ConnectionDisplayName),
		ConnectionDbType:        ptr(r.ConnectionDBType),
		RequiredApprovals:       ptr(int32(r.RequiredApprovals)), //nolint:gosec // checked 0..100 by domain + table constraint
		PayloadDigest:           r.Digest,
		PayloadDigestKeyVersion: &kv,
		RedactedSql:             ptr(r.RedactedSQL),
		// The database stamps submission and auto-approval expiry from one instant after the connection lock, rather than an application timestamp taken before waiting.
		ValiditySeconds: validity.Seconds(),
	}
	cid, err := stringToUUID(string(r.ConnectionID))
	if err != nil {
		return access.RequestView{}, fmt.Errorf("connection id: %w", err)
	}
	var view access.RequestView
	err = s.conns.withTx(ctx, func(q *db.Queries) error {
		// Recheck the pinned policy under the connection lock so submission cannot use a superseded quorum after the policy cascade has run.
		locked, err := s.lockConnection(ctx, q, cid, organizationUUID)
		if err != nil {
			return err
		}
		if locked.CurrentPolicyVersion != r.PolicyVersion {
			return connection.ErrPolicyConflict
		}
		// The target itself is pinned the same way: the digest the pipeline just computed names a configuration, and if a replacement committed in between, this request would land approved-able against a database nobody looked at (ADR-0018).
		if locked.ConfigVersion != r.ConnectionConfigVersion {
			return access.ErrConnectionChanged
		}
		row, err := q.SubmitAccessRequest(ctx, params)
		if err != nil {
			return s.draftGuardError(ctx, q, requestUUID, organizationUUID, err)
		}
		// Use database-stamped submission time and derive system approval time from expires_at − validity so row and audit evidence agree.
		if err := insertEvents(ctx, q, stampEvents(completeAutoApproval(events, row, validity), tsToTime(row.UpdatedAt).UTC())); err != nil {
			return err
		}
		view, err = s.loadRequestViewInTransaction(ctx, q, requestUUID, organizationUUID, pgtype.UUID{})
		return err
	})
	if err != nil {
		return access.RequestView{}, err
	}
	return view, nil
}

// completeAutoApproval derives system approval time from database-stamped expiry and returns copied events without mutating caller metadata.
func completeAutoApproval(events []audit.Event, row db.AccessRequest, validity time.Duration) []audit.Event {
	if !row.ExpiresAt.Valid {
		return events
	}
	expires := tsToTime(row.ExpiresAt).UTC()
	out := make([]audit.Event, len(events))
	copy(out, events)
	for idx := range out {
		if out[idx].ActorService != access.ActorAutoApproval {
			continue
		}
		out[idx].OccurredAt = expires.Add(-validity)
		meta := make(map[string]any, len(out[idx].Metadata)+1)
		for k, v := range out[idx].Metadata {
			meta[k] = v
		}
		meta["expires_at"] = expires.Format(time.RFC3339)
		out[idx].Metadata = meta
	}
	return out
}

// Approve records an eligible decision and transitions when quorum is reached.
func (s *AccessRequestStore) Approve(ctx context.Context, org identity.OrganizationID, id access.RequestID, approver identity.UserID, reason string, validity time.Duration, evt audit.Event) (access.RequestView, error) {
	return s.decide(ctx, org, id, approver, access.DecisionApproved, reason, validity, evt)
}

// Reject records a terminal rejection of a pending request.
func (s *AccessRequestStore) Reject(ctx context.Context, org identity.OrganizationID, id access.RequestID, approver identity.UserID, reason string, evt audit.Event) (access.RequestView, error) {
	return s.decide(ctx, org, id, approver, access.DecisionRejected, reason, 0, evt)
}

// decisionPermission returns the permission required for an approval decision.
func decisionPermission(d access.Decision) string {
	if d == access.DecisionRejected {
		return "requests.reject"
	}
	return "requests.approve"
}

func (s *AccessRequestStore) decide(ctx context.Context, org identity.OrganizationID, id access.RequestID, approver identity.UserID, decision access.Decision, reason string, validity time.Duration, evt audit.Event) (access.RequestView, error) {
	requestUUID, organizationUUID, err := parseRequestAndOrganizationUUIDs(id, org)
	if err != nil {
		return access.RequestView{}, err
	}
	aid, err := stringToUUID(string(approver))
	if err != nil {
		return access.RequestView{}, fmt.Errorf("approver id: %w", err)
	}
	var view access.RequestView
	var refused error
	err = s.conns.withTx(ctx, func(q *db.Queries) error {
		row, err := q.GetAccessRequestForUpdate(ctx, db.GetAccessRequestForUpdateParams{ID: requestUUID, OrganizationID: organizationUUID})
		if err != nil {
			return notFound(err, access.ErrNotFound)
		}
		// Lazy TTL, observed under the lock we now hold: a decision must never land on a request the deadline already took. `refused` carries the refusal out so the expiry transition commits (see observeOverdue).
		expired, err := observeOverdue(ctx, q, requestUUID, organizationUUID, evt)
		if err != nil {
			return err
		}
		if expired {
			refused = access.ErrNotPending
			return nil
		}
		r := toAccessRequest(row)
		if r.State != access.StatePending {
			return access.ErrNotPending
		}
		// Domain validation (shape, self-approval, rejection reason) against the LOCKED row's requester — the race-free enforcement point.
		if _, err := access.NewApproval(id, org, approver, r.RequesterID, decision, reason, time.Now()); err != nil {
			return err
		}
		// Recheck active status and action permission under the membership lock so concurrent revocation cannot authorize a stale approver.
		if _, err := q.LockApproverMembership(ctx, db.LockApproverMembershipParams{
			ApproverID: aid, OrganizationID: organizationUUID,
		}); err != nil {
			return err
		}
		eligible, err := q.ApproverEligible(ctx, db.ApproverEligibleParams{
			ApproverID:     aid,
			OrganizationID: organizationUUID,
			PermissionKey:  decisionPermission(decision),
		})
		if err != nil {
			return err
		}
		if !eligible {
			return access.ErrApproverIneligible
		}
		// The approval's own timestamp is the decision's instant (clock_timestamp() inside the query, not the frozen now()), and everything else this transaction records about the decision is derived from it: the row's updated_at, the validity window, and the audit event's occurred_at.
		decidedAt, err := q.InsertApproval(ctx, db.InsertApprovalParams{
			RequestID:      requestUUID,
			OrganizationID: organizationUUID,
			ApproverID:     aid,
			Decision:       string(decision),
			Reason:         reason,
		})
		if err != nil {
			return onUniqueViolation(err, "approvals_one_decision_per_approver", access.ErrAlreadyDecided)
		}
		decided := tsToTime(decidedAt).UTC()
		evt.OccurredAt = decided

		from := r.State
		switch decision {
		case access.DecisionRejected:
			if r, err = s.transitionAt(ctx, q, requestUUID, organizationUUID, from, access.StateRejected, nil, nil, decided); err != nil {
				return err
			}
		case access.DecisionApproved:
			count, err := q.CountValidApprovals(ctx, db.CountValidApprovalsParams{
				RequestID: requestUUID, OrganizationID: organizationUUID, RequesterID: row.RequesterID,
			})
			if err != nil {
				return err
			}
			if count >= int64(r.RequiredApprovals) {
				// The window opens at the decision, so it is measured from the instant the DB stamped on the approval — not from the app's clock, which is a different clock than the one on the row.
				expires := decided.Add(validity)
				if r, err = s.transitionAt(ctx, q, requestUUID, organizationUUID, from, access.StateApproved, nil, &expires, decided); err != nil {
					return err
				}
			}
		}

		completeRequestEvent(&evt, r, from)
		if err := insertAuditTx(ctx, q, evt); err != nil {
			return err
		}
		view, err = s.loadRequestViewInTransaction(ctx, q, requestUUID, organizationUUID, row.RequesterID)
		return err
	})
	if err != nil {
		return access.RequestView{}, err
	}
	if refused != nil {
		return access.RequestView{}, refused
	}
	return view, nil
}

// Cancel cancels the requester's draft, pending, or approved request.
func (s *AccessRequestStore) Cancel(ctx context.Context, org identity.OrganizationID, id access.RequestID, requester identity.UserID, evt audit.Event) (access.RequestView, error) {
	requestUUID, organizationUUID, err := parseRequestAndOrganizationUUIDs(id, org)
	if err != nil {
		return access.RequestView{}, err
	}
	var view access.RequestView
	var refused error
	err = s.conns.withTx(ctx, func(q *db.Queries) error {
		row, err := q.GetAccessRequestForUpdate(ctx, db.GetAccessRequestForUpdateParams{ID: requestUUID, OrganizationID: organizationUUID})
		if err != nil {
			return notFound(err, access.ErrNotFound)
		}
		r := toAccessRequest(row)
		if r.RequesterID != requester {
			return access.ErrNotFound
		}
		// Lazy TTL, observed under the lock (see observeOverdue). Ownership is checked first so a foreign requester still learns nothing.
		expired, err := observeOverdue(ctx, q, requestUUID, organizationUUID, evt)
		if err != nil {
			return err
		}
		if expired {
			refused = access.ErrNotCancellable
			return nil
		}
		if err := r.ValidateCancellation(); err != nil {
			return err
		}
		from := r.State
		if r, err = s.transition(ctx, q, requestUUID, organizationUUID, from, access.StateCancelled, nil, nil); err != nil {
			return err
		}
		completeRequestEvent(&evt, r, from)
		// The transition stamped the row from the DB clock under the lock; the event names that same instant instead of falling through to now(), which is this transaction's start time (ADR-0009).
		evt.OccurredAt = r.UpdatedAt
		view, err = s.loadRequestViewInTransaction(ctx, q, requestUUID, organizationUUID, row.RequesterID)
		if err != nil {
			return err
		}
		return insertAuditTx(ctx, q, evt)
	})
	if err != nil {
		return access.RequestView{}, err
	}
	if refused != nil {
		return access.RequestView{}, refused
	}
	return view, nil
}

// Get returns an organization-scoped request view and encrypted payload.
func (s *AccessRequestStore) Get(ctx context.Context, org identity.OrganizationID, id access.RequestID) (access.RequestView, access.SealedPayload, error) {
	requestUUID, organizationUUID, err := parseRequestAndOrganizationUUIDs(id, org)
	if err != nil {
		return access.RequestView{}, access.SealedPayload{}, err
	}
	// The row (with its valid-approval count computed in SQL) and the approval list are two statements, so they read ONE snapshot: under Read Committed an approval committing between them answers with a count of zero beside a listed approval — a detail view contradicting itself.
	var view access.RequestView
	var sealed access.SealedPayload
	err = s.conns.withReadSnapshot(ctx, func(_ pgx.Tx, q *db.Queries) error {
		row, err := q.GetAccessRequestView(ctx, db.GetAccessRequestViewParams{ID: requestUUID, OrganizationID: organizationUUID})
		if err != nil {
			return notFound(err, access.ErrNotFound)
		}
		vr := requestViewRow(row)
		if view, err = s.assembleView(ctx, q, vr); err != nil {
			return err
		}
		sealed = toSealedPayload(toBaseRequestRow(vr))
		return nil
	})
	if err != nil {
		return access.RequestView{}, access.SealedPayload{}, err
	}
	return view, sealed, nil
}

// loadRequestViewInTransaction reads request details using the active transaction.
func (s *AccessRequestStore) loadRequestViewInTransaction(ctx context.Context, q *db.Queries, requestUUID, organizationUUID pgtype.UUID, _ pgtype.UUID) (access.RequestView, error) {
	row, err := q.GetAccessRequestView(ctx, db.GetAccessRequestViewParams{ID: requestUUID, OrganizationID: organizationUUID})
	if err != nil {
		return access.RequestView{}, notFound(err, access.ErrNotFound)
	}
	return s.assembleView(ctx, q, requestViewRow(row))
}

// List returns a page of request summaries without approval details.
func (s *AccessRequestStore) List(ctx context.Context, org identity.OrganizationID, p access.ListQuery) (access.RequestPage, error) {
	organizationUUID, err := stringToUUID(string(org))
	if err != nil {
		return access.RequestPage{}, err
	}
	var state *string
	if p.State != "" {
		state = ptr(string(p.State))
	}
	var requester pgtype.UUID
	if p.RequesterID != "" {
		if requester, err = stringToUUID(string(p.RequesterID)); err != nil {
			return access.RequestPage{}, fmt.Errorf("requester id: %w", err)
		}
	}
	limit := int64(p.PageSize)

	// Read rows, total, and page clamp in one snapshot; separate Read Committed statements can disagree (ADR-0018).
	page := p.Page
	var rows []requestViewRow
	var total int64
	err = s.conns.withReadSnapshot(ctx, func(_ pgx.Tx, q *db.Queries) error {
		read := func(pageNumber int) error {
			offset := int64(pageNumber-1) * int64(p.PageSize)
			if p.SortDescending {
				got, err := q.ListAccessRequestsDesc(ctx, db.ListAccessRequestsDescParams{
					OrganizationID: organizationUUID, State: state, RequesterID: requester, PageLimit: limit, RowOffset: offset,
				})
				if err != nil {
					return err
				}
				rows = make([]requestViewRow, len(got))
				for idx, r := range got {
					rows[idx] = requestViewRow(r)
				}
				return nil
			}
			got, err := q.ListAccessRequestsAsc(ctx, db.ListAccessRequestsAscParams{
				OrganizationID: organizationUUID, State: state, RequesterID: requester, PageLimit: limit, RowOffset: offset,
			})
			if err != nil {
				return err
			}
			rows = make([]requestViewRow, len(got))
			for idx, r := range got {
				rows[idx] = requestViewRow(r)
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
		total, err = q.CountAccessRequests(ctx, db.CountAccessRequestsParams{
			OrganizationID: organizationUUID, State: state, RequesterID: requester,
		})
		if err != nil {
			return err
		}
		// Empty because the caller asked past the end: land on the last page that has rows. An empty SET stays on page 1 — there is no page to go back to.
		last := int(total+int64(p.PageSize)-1) / p.PageSize
		if last == 0 {
			page = 1
			return nil
		}
		if page <= last {
			return nil // genuinely empty in range (a filter matching nothing)
		}
		page = last
		return read(page)
	})
	if err != nil {
		return access.RequestPage{}, err
	}
	items := make([]access.RequestView, 0, len(rows))
	for _, r := range rows {
		items = append(items, viewFromRow(r))
	}
	// Page is the page actually READ, which is what the caller must echo: after a clamp it is no longer the page that was asked for.
	return access.RequestPage{Items: items, Page: page, TotalCount: total}, nil
}

// observeOverdue expires a locked request and appends its system audit event.
func observeOverdue(ctx context.Context, q *db.Queries, requestUUID, organizationUUID pgtype.UUID, correlate audit.Event) (bool, error) {
	rows, err := q.ExpireOverdueAccessRequest(ctx, db.ExpireOverdueAccessRequestParams{ID: requestUUID, OrganizationID: organizationUUID})
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		expired := toAccessRequest(row)
		evt := access.ExpiredEvent(expired, access.StateApproved, access.ReasonTTLExpired, access.ActorTTLExpiry, correlate)
		// Use the row’s post-lock observation time for expiry audit so occurred_at cannot precede expires_at.
		evt.OccurredAt = expired.UpdatedAt
		if err := insertAuditTx(ctx, q, evt); err != nil {
			return false, err
		}
	}
	return len(rows) > 0, nil
}

// sweepRequestsForArchive cancels drafts and expires unexecuted requests during archive.
func sweepRequestsForArchive(ctx context.Context, q *db.Queries, cid, organizationUUID pgtype.UUID, at time.Time, correlate audit.Event) error {
	cancelled, err := q.CancelDraftsForConnection(ctx, db.CancelDraftsForConnectionParams{
		ConnectionID: cid, OrganizationID: organizationUUID, At: timeToTS(at),
	})
	if err != nil {
		return err
	}
	for _, row := range cancelled {
		evt := access.CancelledEvent(toAccessRequest(row), access.StateDraft, access.ReasonConnectionArchived, access.ActorArchiveCascade, correlate)
		evt.OccurredAt = at
		if err := insertAuditTx(ctx, q, evt); err != nil {
			return err
		}
	}
	expired, err := q.ExpireLiveRequestsForConnection(ctx, db.ExpireLiveRequestsForConnectionParams{
		ConnectionID: cid, OrganizationID: organizationUUID, StateReason: ptr(string(access.ReasonConnectionArchived)), At: timeToTS(at),
	})
	if err != nil {
		return err
	}
	return insertExpiredEvents(ctx, q, expired, at, access.ReasonConnectionArchived, access.ActorArchiveCascade, correlate)
}

// expireRequestsForPolicyChange expires unexecuted requests in the policy transaction.
func expireRequestsForPolicyChange(ctx context.Context, q *db.Queries, cid, organizationUUID pgtype.UUID, at time.Time, correlate audit.Event) error {
	return expireLiveRequests(ctx, q, cid, organizationUUID, at, access.ReasonPolicyChanged, access.ActorPolicyCascade, correlate)
}

// Config replacement expires existing approvals because they bind the old target and credential. Drafts remain available for submission against the new configuration (ADR-0018).
func expireRequestsForConfigChange(ctx context.Context, q *db.Queries, cid, organizationUUID pgtype.UUID, at time.Time, correlate audit.Event) error {
	return expireLiveRequests(ctx, q, cid, organizationUUID, at, access.ReasonConnectionChanged, access.ActorConfigCascade, correlate)
}

func expireLiveRequests(ctx context.Context, q *db.Queries, cid, organizationUUID pgtype.UUID, at time.Time, reason access.Reason, actor string, correlate audit.Event) error {
	expired, err := q.ExpireLiveRequestsForConnection(ctx, db.ExpireLiveRequestsForConnectionParams{
		ConnectionID: cid, OrganizationID: organizationUUID, StateReason: ptr(string(reason)), At: timeToTS(at),
	})
	if err != nil {
		return err
	}
	return insertExpiredEvents(ctx, q, expired, at, reason, actor, correlate)
}

// insertExpiredEvents records a system expiry event for each affected request.
func insertExpiredEvents(ctx context.Context, q *db.Queries, rows []db.AccessRequest, at time.Time, reason access.Reason, actor string, correlate audit.Event) error {
	for _, row := range rows {
		from := access.StatePending
		if row.ExpiresAt.Valid {
			from = access.StateApproved
		}
		evt := access.ExpiredEvent(toAccessRequest(row), from, reason, actor, correlate)
		// The sweep's observed instant, not the column DEFAULT: now() would be this transaction's start time (see observeCascadeInstant).
		evt.OccurredAt = at
		if err := insertAuditTx(ctx, q, evt); err != nil {
			return err
		}
	}
	return nil
}

// transition applies a guarded state change to a locked request.
func (s *AccessRequestStore) transition(ctx context.Context, q *db.Queries, requestUUID, organizationUUID pgtype.UUID, from, to access.State, reason *access.Reason, expires *time.Time) (access.Request, error) {
	return s.transitionAt(ctx, q, requestUUID, organizationUUID, from, to, reason, expires, time.Time{})
}

// transitionAt applies a guarded state change at the supplied database timestamp.
func (s *AccessRequestStore) transitionAt(ctx context.Context, q *db.Queries, requestUUID, organizationUUID pgtype.UUID, from, to access.State, reason *access.Reason, expires *time.Time, at time.Time) (access.Request, error) {
	params := db.TransitionAccessRequestParams{
		ID:             requestUUID,
		OrganizationID: organizationUUID,
		FromState:      string(from),
		NextState:      string(to),
	}
	if reason != nil {
		params.StateReason = ptr(string(*reason))
	}
	if expires != nil {
		params.ExpiresAt = timeToTS(*expires)
	}
	if !at.IsZero() {
		params.DecidedAt = timeToTS(at)
	}
	row, err := q.TransitionAccessRequest(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return access.Request{}, fmt.Errorf("access: transition %s→%s lost its guard under lock: %w", from, to, access.ErrConflict)
		}
		return access.Request{}, err
	}
	return toAccessRequest(row), nil
}

// draftGuardError distinguishes missing requests, changed state, and version conflicts.
func (s *AccessRequestStore) draftGuardError(ctx context.Context, q *db.Queries, requestUUID, organizationUUID pgtype.UUID, err error) error {
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	row, getErr := q.GetAccessRequest(ctx, db.GetAccessRequestParams{ID: requestUUID, OrganizationID: organizationUUID})
	if getErr != nil {
		return notFound(getErr, access.ErrNotFound)
	}
	if access.State(row.State) != access.StateDraft {
		return access.ErrNotDraft
	}
	return access.ErrConflict
}

// assembleView attaches approval details and current validity to a request view.
func (s *AccessRequestStore) assembleView(ctx context.Context, q *db.Queries, row requestViewRow) (access.RequestView, error) {
	view := viewFromRow(row)
	approvals, err := q.ListApprovalsForRequest(ctx, db.ListApprovalsForRequestParams{
		RequestID: row.ID, OrganizationID: row.OrganizationID, RequesterID: row.RequesterID,
	})
	if err != nil {
		return access.RequestView{}, err
	}
	for _, a := range approvals {
		view.Approvals = append(view.Approvals, access.ApprovalView{
			Approval: access.Approval{
				ID:             uuidToString(a.ID),
				RequestID:      access.RequestID(uuidToString(a.RequestID)),
				OrganizationID: identity.OrganizationID(uuidToString(a.OrganizationID)),
				ApproverID:     identity.UserID(uuidToString(a.ApproverID)),
				Decision:       access.Decision(a.Decision),
				Reason:         a.Reason,
				DecidedAt:      tsToTime(a.DecidedAt),
			},
			ApproverEmail:       a.ApproverEmail,
			ApproverDisplayName: a.ApproverDisplayName,
			Valid:               a.Valid,
		})
	}
	return view, nil
}

// completeRequestEvent fills audit fields from the request transition.
func completeRequestEvent(evt *audit.Event, r access.Request, from access.State) {
	evt.OrganizationID = r.OrganizationID
	evt.TargetID = string(r.ID)
	evt.PreviousState = string(from)
	evt.NextState = string(r.State)
	evt.ConnectionID = string(r.ConnectionID)
	evt.QueryType = string(r.Class)
	evt.PayloadDigest = r.Digest
	evt.PayloadDigestKeyVersion = r.DigestKeyVersion
	meta := map[string]any{"connection_id": string(r.ConnectionID)}
	if r.RedactedSQL != "" {
		meta["redacted_sql"] = r.RedactedSQL
	}
	evt.Metadata = meta
}

// requestViewRow shares the selected fields of request detail and list rows.
type requestViewRow struct {
	ID                      pgtype.UUID
	OrganizationID          pgtype.UUID
	ConnectionID            pgtype.UUID
	RequesterID             pgtype.UUID
	State                   string
	StateReason             *string
	PayloadKeyVersion       int32
	PayloadWrappedDek       []byte
	PayloadNonce            []byte
	PayloadCiphertext       []byte
	PayloadDigest           []byte
	PayloadDigestKeyVersion *int32
	RedactedSql             *string
	StatementClass          *string
	PolicyVersion           *int64
	RequiredApprovals       *int32
	SubmittedAt             pgtype.Timestamptz
	// Field ORDER mirrors the table: these structs are converted to and from the generated row types, which sqlc lays out in `select *` order.
	ConnectionConfigVersion *int64
	ConnectionFingerprint   *string
	ConnectionDisplayName   *string
	ConnectionDbType        *string
	ExpiresAt               pgtype.Timestamptz
	Version                 int64
	CreatedAt               pgtype.Timestamptz
	UpdatedAt               pgtype.Timestamptz
	Title                   string
	RequesterEmail          string
	RequesterDisplayName    string
	ConnectionName          string
	// EffectiveState/EffectiveReason are the badge, computed by the query from the same now() it filtered and counted with (ADR-0018 §92). The response carries them through instead of deciding again from the app's clock.
	EffectiveState  string
	EffectiveReason string
	ValidApprovals  int64
	// TotalCount is the page's total, computed by the SAME query as the rows (count(*) over ()) so the two cannot come from different snapshots. On the detail path the expression is literally 1 and the field goes unused.
	TotalCount int64
}

func viewFromRow(r requestViewRow) access.RequestView {
	return access.RequestView{
		Request:              toAccessRequest(toBaseRequestRow(r)),
		RequesterEmail:       r.RequesterEmail,
		RequesterDisplayName: r.RequesterDisplayName,
		ConnectionName:       r.ConnectionName,
		// Straight from the query — see requestViewRow. Deciding the badge again here (or in the service) would reintroduce the second clock.
		EffectiveState:  access.State(r.EffectiveState),
		EffectiveReason: access.Reason(r.EffectiveReason),
		ValidApprovals:  int(r.ValidApprovals),
	}
}

// toBaseRequestRow extracts the base request row from joined display fields.
func toBaseRequestRow(r requestViewRow) db.AccessRequest {
	return db.AccessRequest{
		Title:                   r.Title,
		ID:                      r.ID,
		OrganizationID:          r.OrganizationID,
		ConnectionID:            r.ConnectionID,
		RequesterID:             r.RequesterID,
		State:                   r.State,
		StateReason:             r.StateReason,
		PayloadKeyVersion:       r.PayloadKeyVersion,
		PayloadWrappedDek:       r.PayloadWrappedDek,
		PayloadNonce:            r.PayloadNonce,
		PayloadCiphertext:       r.PayloadCiphertext,
		PayloadDigest:           r.PayloadDigest,
		PayloadDigestKeyVersion: r.PayloadDigestKeyVersion,
		RedactedSql:             r.RedactedSql,
		StatementClass:          r.StatementClass,
		PolicyVersion:           r.PolicyVersion,
		RequiredApprovals:       r.RequiredApprovals,
		SubmittedAt:             r.SubmittedAt,
		ConnectionConfigVersion: r.ConnectionConfigVersion,
		ConnectionFingerprint:   r.ConnectionFingerprint,
		ConnectionDisplayName:   r.ConnectionDisplayName,
		ConnectionDbType:        r.ConnectionDbType,
		ExpiresAt:               r.ExpiresAt,
		Version:                 r.Version,
		CreatedAt:               r.CreatedAt,
		UpdatedAt:               r.UpdatedAt,
	}
}

func toAccessRequest(row db.AccessRequest) access.Request {
	r := access.Request{
		Title:          row.Title,
		ID:             access.RequestID(uuidToString(row.ID)),
		OrganizationID: identity.OrganizationID(uuidToString(row.OrganizationID)),
		ConnectionID:   connection.ConnectionID(uuidToString(row.ConnectionID)),
		RequesterID:    identity.UserID(uuidToString(row.RequesterID)),
		State:          access.State(row.State),
		Version:        row.Version,
		CreatedAt:      tsToTime(row.CreatedAt),
		UpdatedAt:      tsToTime(row.UpdatedAt),
	}
	if row.StateReason != nil {
		r.Reason = access.Reason(*row.StateReason)
	}
	r.Digest = row.PayloadDigest
	if row.PayloadDigestKeyVersion != nil {
		r.DigestKeyVersion = uint32(*row.PayloadDigestKeyVersion) //nolint:gosec // CHECK > 0
	}
	if row.RedactedSql != nil {
		r.RedactedSQL = *row.RedactedSql
	}
	if row.StatementClass != nil {
		r.Class = connection.StatementClass(*row.StatementClass)
	}
	if row.PolicyVersion != nil {
		r.PolicyVersion = *row.PolicyVersion
	}
	if row.RequiredApprovals != nil {
		r.RequiredApprovals = int(*row.RequiredApprovals)
	}
	if row.ConnectionConfigVersion != nil {
		r.ConnectionConfigVersion = *row.ConnectionConfigVersion
	}
	if row.ConnectionFingerprint != nil {
		r.ConnectionFingerprint = *row.ConnectionFingerprint
	}
	if row.ConnectionDisplayName != nil {
		r.ConnectionDisplayName = *row.ConnectionDisplayName
	}
	if row.ConnectionDbType != nil {
		r.ConnectionDBType = *row.ConnectionDbType
	}
	if row.SubmittedAt.Valid {
		t := tsToTime(row.SubmittedAt)
		r.SubmittedAt = &t
	}
	if row.ExpiresAt.Valid {
		t := tsToTime(row.ExpiresAt)
		r.ExpiresAt = &t
	}
	return r
}

func toSealedPayload(row db.AccessRequest) access.SealedPayload {
	return access.SealedPayload{
		KeyVersion: uint32(row.PayloadKeyVersion), //nolint:gosec // CHECK > 0
		WrappedDEK: row.PayloadWrappedDek,
		Nonce:      row.PayloadNonce,
		Ciphertext: row.PayloadCiphertext,
	}
}

func parseRequestAndOrganizationUUIDs(id access.RequestID, org identity.OrganizationID) (pgtype.UUID, pgtype.UUID, error) {
	requestUUID, err := stringToUUID(string(id))
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, fmt.Errorf("request id: %w", err)
	}
	organizationUUID, err := stringToUUID(string(org))
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, fmt.Errorf("organization id: %w", err)
	}
	return requestUUID, organizationUUID, nil
}

func ptr[T any](v T) *T { return &v }
