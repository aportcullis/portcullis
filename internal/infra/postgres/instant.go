package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// Read the database clock after acquiring mutation locks so rows and audit events follow execution order; now() and inline clock_timestamp() can predate lock waits (ADR-0009).

// observeInstant reads the stamp once the caller's locks are already held. Callers that hold every row they are about to write use this directly; a cascade, whose targets are not covered by the lock it took, goes through observeCascadeInstant.
func observeInstant(ctx context.Context, q *db.Queries) (time.Time, error) {
	at, err := q.ObserveWallClock(ctx)
	if err != nil {
		return time.Time{}, err
	}
	return tsToTime(at).UTC(), nil
}

// observeConfigChangeInstant is observeCascadeInstant for the config-replacement cascade, which sweeps pending/approved only — so it locks only those rows and leaves a concurrent draft edit free to proceed.
func observeConfigChangeInstant(ctx context.Context, q *db.Queries, cid, oid pgtype.UUID) (time.Time, error) {
	if _, err := q.LockLiveRequestsForConnection(ctx, db.LockLiveRequestsForConnectionParams{
		ConnectionID: cid, OrganizationID: oid,
	}); err != nil {
		return time.Time{}, err
	}
	return observeInstant(ctx, q)
}

// observeCascadeInstant locks every request row the sweep is about to touch and then observes. The connection lock its caller already holds does not cover those rows: only CreateDraft and Submit lock the connection, while a decision or a cancel locks the request row alone.
func observeCascadeInstant(ctx context.Context, q *db.Queries, cid, oid pgtype.UUID) (time.Time, error) {
	if _, err := q.LockSweptRequestsForConnection(ctx, db.LockSweptRequestsForConnectionParams{
		ConnectionID: cid, OrganizationID: oid,
	}); err != nil {
		return time.Time{}, err
	}
	return observeInstant(ctx, q)
}

// stampEvents dates a transaction's events with its observed instant. An event that already carries a more precise moment of its own — a decision's decided_at, an auto-approval derived from expires_at — keeps it: those name the instant of a specific row, not of the transaction.
func stampEvents(events []audit.Event, at time.Time) []audit.Event {
	out := make([]audit.Event, len(events))
	copy(out, events)
	for idx := range out {
		if out[idx].OccurredAt.IsZero() {
			out[idx].OccurredAt = at
		}
	}
	return out
}
