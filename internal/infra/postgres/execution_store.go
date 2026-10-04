package postgres

import (
	"context"
	"crypto/hmac"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// AcquireExecution reserves one approved request and records its start atomically.
func (s *AccessRequestStore) AcquireExecution(ctx context.Context, org identity.OrganizationID, requestID access.RequestID, requester identity.UserID, owner, attemptID string, digest []byte, event audit.Event) (access.ExecutionLease, error) {
	requestUUID, organizationUUID, err := parseRequestAndOrganizationUUIDs(requestID, org)
	if err != nil {
		return access.ExecutionLease{}, err
	}
	attemptUUID, err := stringToUUID(attemptID)
	if err != nil || owner == "" || len(owner) > 128 {
		return access.ExecutionLease{}, access.ErrInvalidRequest
	}
	var lease access.ExecutionLease
	var refused error
	err = s.conns.withTx(ctx, func(q *db.Queries) error {
		before, err := q.GetAccessRequest(ctx, db.GetAccessRequestParams{ID: requestUUID, OrganizationID: organizationUUID})
		if err != nil {
			return notFound(err, access.ErrNotFound)
		}
		// Match cascade lock order: connection first, then request.
		target, err := s.lockConnection(ctx, q, before.ConnectionID, organizationUUID)
		if err != nil {
			return err
		}
		row, err := q.GetAccessRequestForUpdate(ctx, db.GetAccessRequestForUpdateParams{ID: requestUUID, OrganizationID: organizationUUID})
		if err != nil {
			return err
		}
		r := toAccessRequest(row)
		if r.RequesterID != requester {
			return access.ErrNotFound
		}
		if r.State != access.StateApproved {
			return access.ErrNotExecutable
		}
		at, err := observeInstant(ctx, q)
		if err != nil {
			return err
		}
		reason := access.Reason("")
		switch {
		case r.ApprovalExpiredAt(at):
			reason = access.ReasonTTLExpired
		case r.PolicyVersion != target.CurrentPolicyVersion:
			reason = access.ReasonPolicyChanged
		case r.ConnectionConfigVersion != target.ConfigVersion:
			reason = access.ReasonConnectionChanged
		}
		count, err := q.CountValidApprovals(ctx, db.CountValidApprovalsParams{RequestID: requestUUID, OrganizationID: organizationUUID, RequesterID: row.RequesterID})
		if err != nil {
			return err
		}
		if reason == "" && count < int64(r.RequiredApprovals) {
			reason = access.ReasonApprovalInvalidated
		}
		if reason != "" {
			if _, err = s.transitionAt(ctx, q, requestUUID, organizationUUID, access.StateApproved, access.StateExpired, &reason, nil, at); err != nil {
				return err
			}
			evt := access.ExpiredEvent(r, access.StateApproved, reason, "system:execution-gate", event)
			evt.OccurredAt = at
			refused = access.ErrNotExecutable
			return insertAuditTx(ctx, q, evt)
		}
		eligible, err := q.ApproverEligible(ctx, db.ApproverEligibleParams{ApproverID: row.RequesterID, OrganizationID: organizationUUID, PermissionKey: "requests.execute"})
		if err != nil {
			return err
		}
		if !eligible {
			return access.ErrNotExecutable
		}
		if !hmac.Equal(r.Digest, digest) {
			return access.ErrPayloadIntegrity
		}
		recorded, err := q.InsertQueryExecution(ctx, db.InsertQueryExecutionParams{RequestID: requestUUID, OrganizationID: organizationUUID, Owner: owner, AttemptID: attemptUUID, At: timeToTS(at), LeaseMilliseconds: access.ExecutionLeaseDuration.Milliseconds()})
		if err != nil {
			return err
		}
		if _, err = s.transitionAt(ctx, q, requestUUID, organizationUUID, access.StateApproved, access.StateExecuting, nil, nil, at); err != nil {
			return err
		}
		evt := executionEvent(r, event, audit.ActionExecutionStarted, access.StateApproved, access.StateExecuting)
		evt.OccurredAt = at
		if err = insertAuditTx(ctx, q, evt); err != nil {
			return err
		}
		lease = access.ExecutionLease{RequestID: requestID, Owner: owner, AttemptID: attemptID, Heartbeat: tsToTime(recorded.Heartbeat), Deadline: tsToTime(recorded.Deadline)}
		return nil
	})
	if err != nil {
		return access.ExecutionLease{}, err
	}
	if refused != nil {
		return access.ExecutionLease{}, refused
	}
	return lease, nil
}

// HeartbeatExecution extends only the live lease owned by this attempt.
func (s *AccessRequestStore) HeartbeatExecution(ctx context.Context, org identity.OrganizationID, lease access.ExecutionLease) error {
	rid, oid, err := parseRequestAndOrganizationUUIDs(lease.RequestID, org)
	if err != nil {
		return err
	}
	aid, err := stringToUUID(lease.AttemptID)
	if err != nil {
		return err
	}
	return s.conns.withTx(ctx, func(q *db.Queries) error {
		row, err := q.GetAccessRequestForUpdate(ctx, db.GetAccessRequestForUpdateParams{ID: rid, OrganizationID: oid})
		if err != nil {
			return notFound(err, access.ErrNotFound)
		}
		if access.State(row.State) != access.StateExecuting {
			return access.ErrLeaseLost
		}
		if _, err = q.LockQueryExecution(ctx, db.LockQueryExecutionParams{RequestID: rid, OrganizationID: oid}); err != nil {
			return err
		}
		at, err := observeInstant(ctx, q)
		if err != nil {
			return err
		}
		count, err := q.HeartbeatQueryExecution(ctx, db.HeartbeatQueryExecutionParams{At: timeToTS(at), LeaseMilliseconds: access.ExecutionLeaseDuration.Milliseconds(), RequestID: rid, OrganizationID: oid, Owner: lease.Owner, AttemptID: aid})
		if err != nil {
			return err
		}
		if count != 1 {
			return access.ErrLeaseLost
		}
		return nil
	})
}

// CompleteExecution fences terminal writes and records late reports without rewriting history.
func (s *AccessRequestStore) CompleteExecution(ctx context.Context, org identity.OrganizationID, lease access.ExecutionLease, completion access.ExecutionCompletion, event audit.Event) error {
	if !completion.Valid() || completion.DurationMilliseconds < 0 || completion.RowCount < 0 || completion.ByteCount < 0 {
		return access.ErrInvalidRequest
	}
	rid, oid, err := parseRequestAndOrganizationUUIDs(lease.RequestID, org)
	if err != nil {
		return err
	}
	aid, err := stringToUUID(lease.AttemptID)
	if err != nil {
		return err
	}
	var lost bool
	err = s.conns.withTx(ctx, func(q *db.Queries) error {
		row, err := q.GetAccessRequestForUpdate(ctx, db.GetAccessRequestForUpdateParams{ID: rid, OrganizationID: oid})
		if err != nil {
			return notFound(err, access.ErrNotFound)
		}
		recorded, err := q.LockQueryExecution(ctx, db.LockQueryExecutionParams{RequestID: rid, OrganizationID: oid})
		if err != nil {
			return notFound(err, access.ErrNotFound)
		}
		// A different caller cannot append a fabricated late report for the owning attempt.
		if recorded.Owner != lease.Owner || recorded.AttemptID != aid {
			return access.ErrLeaseLost
		}
		at, err := observeInstant(ctx, q)
		if err != nil {
			return err
		}
		r := toAccessRequest(row)
		if r.State == access.StateExecuting && recorded.Outcome == nil && !at.Before(tsToTime(recorded.Deadline)) {
			if err = s.finishExecutionAt(ctx, q, r, recorded, access.ExecutionCompletion{State: access.StateOutcomeUnknown}, reconcilerEvent(), at); err != nil {
				return err
			}
			r.State = access.StateOutcomeUnknown
		}
		if r.State != access.StateExecuting || recorded.Outcome != nil {
			evt := executionEvent(r, event, audit.ActionLateCompletionObserved, r.State, r.State)
			evt.OccurredAt = at
			evt.Metadata["observed_outcome"] = string(completion.State)
			lost = true
			return insertAuditTx(ctx, q, evt)
		}
		return s.finishExecutionAt(ctx, q, r, recorded, completion, event, at)
	})
	if err != nil {
		return err
	}
	if lost {
		return access.ErrLeaseLost
	}
	return nil
}

// ReconcileExecutions records unknown outcomes for up to batchSize expired owners after the cursor, in deadline order, without retrying SQL. A failing or locked attempt is counted and passed over; only a listing failure or an ended context fails the batch.
func (s *AccessRequestStore) ReconcileExecutions(ctx context.Context, org identity.OrganizationID, after access.ReconcileCursor, batchSize int) (access.ReconcileBatch, error) {
	oid, err := stringToUUID(string(org))
	if err != nil {
		return access.ReconcileBatch{}, err
	}
	if batchSize < 1 || batchSize > math.MaxInt32 {
		return access.ReconcileBatch{}, access.ErrInvalidRequest
	}
	params := db.ListOverdueExecutionsParams{OrganizationID: oid, BatchSize: int32(batchSize)}
	if !after.IsStart() {
		afterID, err := stringToUUID(string(after.RequestID))
		if err != nil {
			return access.ReconcileBatch{}, access.ErrInvalidRequest
		}
		params.AfterDeadline, params.AfterRequestID = timeToTS(after.Deadline), afterID
	}
	overdue, err := s.conns.q.ListOverdueExecutions(ctx, params)
	if err != nil {
		return access.ReconcileBatch{}, err
	}
	batch := access.ReconcileBatch{Listed: len(overdue), Next: after}
	for _, attempt := range overdue {
		batch.Next = access.ReconcileCursor{Deadline: tsToTime(attempt.Deadline), RequestID: access.RequestID(uuidToString(attempt.RequestID))}
		outcome, err := s.reconcileOverdueExecution(ctx, oid, attempt.RequestID)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return batch, ctxErr
			}
			batch.Failed++
			if batch.FirstFailure == nil {
				batch.FirstFailure = err
			}
			continue
		}
		switch outcome {
		case reconcileRecovered:
			batch.Recovered++
		case reconcileSkippedLocked:
			batch.Skipped++
		}
	}
	return batch, nil
}

// reconcileOutcome says what one overdue attempt's recovery transaction did.
type reconcileOutcome uint8

const (
	reconcileUnchanged reconcileOutcome = iota
	reconcileRecovered
	reconcileSkippedLocked
)

// reconcileOverdueExecution records outcome_unknown for one still-overdue attempt, locking request then execution like completion and skipping a request another transaction holds.
func (s *AccessRequestStore) reconcileOverdueExecution(ctx context.Context, oid, rid pgtype.UUID) (reconcileOutcome, error) {
	outcome := reconcileUnchanged
	err := s.conns.withTx(ctx, func(q *db.Queries) error {
		row, err := q.LockAccessRequestSkipLocked(ctx, db.LockAccessRequestSkipLockedParams{ID: rid, OrganizationID: oid})
		if errors.Is(err, pgx.ErrNoRows) {
			outcome = reconcileSkippedLocked
			return nil
		}
		if err != nil {
			return err
		}
		recorded, err := q.LockQueryExecution(ctx, db.LockQueryExecutionParams{RequestID: rid, OrganizationID: oid})
		if err != nil {
			return err
		}
		at, err := observeInstant(ctx, q)
		if err != nil {
			return err
		}
		if access.State(row.State) != access.StateExecuting || recorded.Outcome != nil || at.Before(tsToTime(recorded.Deadline)) {
			return nil
		}
		if err = s.finishExecutionAt(ctx, q, toAccessRequest(row), recorded, access.ExecutionCompletion{State: access.StateOutcomeUnknown}, reconcilerEvent(), at); err != nil {
			return err
		}
		outcome = reconcileRecovered
		return nil
	})
	return outcome, err
}

func (s *AccessRequestStore) finishExecutionAt(ctx context.Context, q *db.Queries, r access.Request, recorded db.QueryExecution, c access.ExecutionCompletion, event audit.Event, at time.Time) error {
	var resultID pgtype.UUID
	var expiry pgtype.Timestamptz
	if c.ResultID != "" {
		var err error
		resultID, err = stringToUUID(c.ResultID)
		if err != nil {
			return err
		}
		if c.ResultExpiresAt == nil {
			return access.ErrInvalidRequest
		}
		expiry = timeToTS(*c.ResultExpiresAt)
	}
	count, err := q.FinishQueryExecution(ctx, db.FinishQueryExecutionParams{Outcome: ptr(string(c.State)), At: timeToTS(at), RowsAffected: c.RowsAffected, DurationMs: c.DurationMilliseconds, ResultID: resultID, ResultExpiresAt: expiry, RowCount: c.RowCount, ByteCount: c.ByteCount, Truncated: c.Truncated, RequestID: recorded.RequestID, OrganizationID: recorded.OrganizationID, Owner: recorded.Owner, AttemptID: recorded.AttemptID})
	if err != nil {
		return err
	}
	if count != 1 {
		return access.ErrLeaseLost
	}
	if _, err = s.transitionAt(ctx, q, recorded.RequestID, recorded.OrganizationID, access.StateExecuting, c.State, nil, nil, at); err != nil {
		return err
	}
	evt := executionEvent(r, event, audit.ActionExecutionFinished, access.StateExecuting, c.State)
	evt.OccurredAt = at
	evt.Metadata["rows_affected"] = c.RowsAffected
	evt.Metadata["duration_ms"] = c.DurationMilliseconds
	evt.RowsAffected = &c.RowsAffected
	evt.DurationMilliseconds = &c.DurationMilliseconds
	if c.ResultUnavailableReason != "" {
		evt.Metadata["result_unavailable_reason"] = string(c.ResultUnavailableReason)
	}
	if c.InterruptionCause != "" {
		evt.Metadata["interruption_cause"] = string(c.InterruptionCause)
	}
	if c.State != access.StateSucceeded {
		evt.Outcome = audit.OutcomeFailed
	}
	return insertAuditTx(ctx, q, evt)
}

func executionEvent(r access.Request, event audit.Event, action audit.Action, from, to access.State) audit.Event {
	event.OrganizationID = r.OrganizationID
	event.Action = action
	event.TargetType = audit.TargetTypeAccessRequest
	event.TargetID = string(r.ID)
	event.Outcome = audit.OutcomeSucceeded
	event.PreviousState = string(from)
	event.NextState = string(to)
	event.ConnectionID = string(r.ConnectionID)
	event.QueryType = string(r.Class)
	event.PayloadDigest = r.Digest
	event.PayloadDigestKeyVersion = r.DigestKeyVersion
	event.Metadata = map[string]any{}
	if r.RedactedSQL != "" {
		event.Metadata["redacted_sql"] = r.RedactedSQL
	}
	return event
}

func reconcilerEvent() audit.Event {
	return audit.Event{ActorType: audit.ActorSystem, ActorService: "system:reconciler"}
}

// RecordExecutionRefusal preserves admission evidence without consuming approval.
func (s *AccessRequestStore) RecordExecutionRefusal(ctx context.Context, org identity.OrganizationID, id access.RequestID, requester identity.UserID, reason string, event audit.Event) error {
	rid, oid, err := parseRequestAndOrganizationUUIDs(id, org)
	if err != nil {
		return err
	}
	return s.conns.withTx(ctx, func(q *db.Queries) error {
		row, err := q.GetAccessRequestForUpdate(ctx, db.GetAccessRequestForUpdateParams{ID: rid, OrganizationID: oid})
		if err != nil {
			return notFound(err, access.ErrNotFound)
		}
		r := toAccessRequest(row)
		if r.RequesterID != requester {
			return access.ErrNotFound
		}
		at, err := observeInstant(ctx, q)
		if err != nil {
			return err
		}
		evt := executionEvent(r, event, audit.Action("EXECUTION_REJECTED"), r.State, r.State)
		evt.OccurredAt = at
		evt.Outcome = audit.OutcomeFailed
		evt.Metadata["reason"] = reason
		if reason == "cancel_requested" {
			evt.Action = audit.Action("EXECUTION_CANCEL_REQUESTED")
			evt.Outcome = audit.OutcomeSucceeded
		}
		return insertAuditTx(ctx, q, evt)
	})
}

// GetExecution reads durable execution metadata from the caller's organization.
func (s *AccessRequestStore) GetExecution(ctx context.Context, org identity.OrganizationID, id access.RequestID) (access.ExecutionCompletion, error) {
	rid, oid, err := parseRequestAndOrganizationUUIDs(id, org)
	if err != nil {
		return access.ExecutionCompletion{}, err
	}
	row, err := s.conns.q.GetQueryExecution(ctx, db.GetQueryExecutionParams{RequestID: rid, OrganizationID: oid})
	if err != nil {
		return access.ExecutionCompletion{}, notFound(err, access.ErrNotFound)
	}
	state := access.StateExecuting
	if row.Outcome != nil {
		state = access.State(*row.Outcome)
	}
	completion := access.ExecutionCompletion{State: state, RowsAffected: row.RowsAffected, DurationMilliseconds: row.DurationMs, RowCount: row.RowCount, ByteCount: row.ByteCount, Truncated: row.Truncated}
	if row.ResultID.Valid {
		completion.ResultID = uuidToString(row.ResultID)
		expiry := tsToTime(row.ResultExpiresAt)
		completion.ResultExpiresAt = &expiry
	}
	return completion, nil
}
