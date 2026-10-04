package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// ConnectionPolicyStore atomically advances the version pointer and appends immutable policy and audit rows (ADR-0015).
type ConnectionPolicyStore struct {
	// conns is reused for org resolution, the withTx helper, and the zero-rowcount disambiguation helpers — same pool, same queries.
	conns *ConnectionStore
}

// NewConnectionPolicyStore builds the store on a connection pool.
func NewConnectionPolicyStore(pool *pgxpool.Pool) *ConnectionPolicyStore {
	return &ConnectionPolicyStore{conns: NewConnectionStore(pool)}
}

// DefaultOrganizationID resolves the single self-hosted organization.
func (s *ConnectionPolicyStore) DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error) {
	return s.conns.DefaultOrganizationID(ctx)
}

// GetCurrent returns the connection's current policy snapshot. Archived connections keep answering — the policy is part of the historical snapshot.
func (s *ConnectionPolicyStore) GetCurrent(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (connection.Policy, error) {
	cid, oid, err := connIDs(id, org)
	if err != nil {
		return connection.Policy{}, err
	}
	row, err := s.conns.q.GetCurrentConnectionPolicy(ctx, db.GetCurrentConnectionPolicyParams{ID: cid, OrganizationID: oid})
	if err != nil {
		return connection.Policy{}, notFound(err, connection.ErrNotFound)
	}
	return toPolicy(row), nil
}

// UpdatePolicy appends next as the new current version. The pointer bump is the optimistic guard: zero rows disambiguates into ErrNotFound / ErrArchived / ErrPolicyConflict; the (connection_id, version) PK and the deferred FK are the structural backstops. Audit events commit in the same transaction.
func (s *ConnectionPolicyStore) UpdatePolicy(ctx context.Context, next connection.Policy, expectedVersion int64, events ...audit.Event) (connection.Policy, error) {
	cid, oid, err := connIDs(next.ConnectionID, next.OrganizationID)
	if err != nil {
		return connection.Policy{}, err
	}
	params, err := policyInsertParams(next)
	if err != nil {
		return connection.Policy{}, err
	}
	stored := next
	err = s.conns.withTx(ctx, func(q *db.Queries) error {
		bumped, err := q.BumpConnectionPolicyVersion(ctx, db.BumpConnectionPolicyVersionParams{
			ID:              cid,
			OrganizationID:  oid,
			ExpectedVersion: expectedVersion,
		})
		if err != nil {
			return s.missingArchivedOrPolicyConflict(ctx, q, cid, oid, err)
		}
		// The pointer the bump just wrote is the only source of the new version number; the caller's Version is ignored so the snapshot and the pointer cannot disagree.
		params.Version = bumped.CurrentPolicyVersion
		stored.Version = bumped.CurrentPolicyVersion
		// The bump above is this transaction's wait: it UPDATEs the connection row, so it parks behind any request write holding it FOR SHARE. The instant is therefore observed HERE, after the wait, and dates EVERYTHING this transaction writes — the policy snapshot included (ADR-0009).
		at, err := observeCascadeInstant(ctx, q, cid, oid)
		if err != nil {
			return err
		}
		// The caller's created_at came from the application clock before this transaction existed; it is an input to domain validation, not the moment this version came into being. The observed instant wins, and it is what the caller gets back.
		params.At = timeToTS(at)
		stored.CreatedAt = at
		if err := q.InsertConnectionPolicyVersion(ctx, params); err != nil {
			return err
		}
		events = stampEvents(events, at)
		// The designated hook point (ADR-0015 §Deferred, discharged by ADR-0018): expire the connection's un-executed pending/approved requests in this same transaction, so no old quorum can approve against the new policy.
		correlate := audit.Event{}
		if len(events) > 0 {
			correlate = events[0]
		}
		if err := expireRequestsForPolicyChange(ctx, q, cid, oid, at, correlate); err != nil {
			return err
		}
		return insertEvents(ctx, q, next.OrganizationID, events)
	})
	if err != nil {
		return connection.Policy{}, err
	}
	return stored, nil
}

// missingArchivedOrPolicyConflict mirrors missingArchivedOrConflict but maps the "still exists, still active" case to ErrPolicyConflict — the policy pointer moved after the caller's read (a distinct message from a descriptor conflict).
func (s *ConnectionPolicyStore) missingArchivedOrPolicyConflict(ctx context.Context, q *db.Queries, id, org pgtype.UUID, err error) error {
	mapped := s.conns.missingArchivedOrConflict(ctx, q, id, org, err)
	if errors.Is(mapped, connection.ErrConflict) {
		return connection.ErrPolicyConflict
	}
	return mapped
}

// policyInsertParams converts a validated domain policy into the append-only insert row. Shared with ConnectionStore.Create, which inserts the v1 default alongside the connection.
func policyInsertParams(p connection.Policy) (db.InsertConnectionPolicyVersionParams, error) {
	cid, oid, err := connIDs(p.ConnectionID, p.OrganizationID)
	if err != nil {
		return db.InsertConnectionPolicyVersionParams{}, err
	}
	createdBy, err := stringToUUID(string(p.CreatedBy))
	if err != nil {
		return db.InsertConnectionPolicyVersionParams{}, fmt.Errorf("created_by: %w", err)
	}
	return db.InsertConnectionPolicyVersionParams{
		ConnectionID:           cid,
		OrganizationID:         oid,
		Version:                p.Version,
		ReadAllowed:            p.Read.Allowed,
		WriteAllowed:           p.Write.Allowed,
		DdlAllowed:             p.DDL.Allowed,
		ReadRequiredApprovals:  int32(p.Read.RequiredApprovals),     //nolint:gosec // checked 0..100 by domain + table constraint
		WriteRequiredApprovals: int32(p.Write.RequiredApprovals),    //nolint:gosec // checked 0..100 by domain + table constraint
		DdlRequiredApprovals:   int32(p.DDL.RequiredApprovals),      //nolint:gosec // checked 0..100 by domain + table constraint
		QueryTimeoutSeconds:    int32(p.Limits.QueryTimeoutSeconds), //nolint:gosec // checked 1..300 by domain + table constraint
		MaxRows:                int32(p.Limits.MaxRows),             //nolint:gosec // checked 1..10000 by domain + table constraint
		MaxResultBytes:         p.Limits.MaxResultBytes,
		CreatedBy:              createdBy,
		// At is filled by the caller from its observed instant; p.CreatedAt is domain-validation input, not the moment this version came into being (ADR-0009).
	}, nil
}

func toPolicy(row db.ConnectionPolicyVersion) connection.Policy {
	return connection.Policy{
		ConnectionID:   connection.ConnectionID(uuidToString(row.ConnectionID)),
		OrganizationID: identity.OrganizationID(uuidToString(row.OrganizationID)),
		Version:        row.Version,
		Read:           connection.ClassRule{Allowed: row.ReadAllowed, RequiredApprovals: int(row.ReadRequiredApprovals)},
		Write:          connection.ClassRule{Allowed: row.WriteAllowed, RequiredApprovals: int(row.WriteRequiredApprovals)},
		DDL:            connection.ClassRule{Allowed: row.DdlAllowed, RequiredApprovals: int(row.DdlRequiredApprovals)},
		Limits: connection.Limits{
			QueryTimeoutSeconds: int(row.QueryTimeoutSeconds),
			MaxRows:             int(row.MaxRows),
			MaxResultBytes:      row.MaxResultBytes,
		},
		CreatedBy: identity.UserID(uuidToString(row.CreatedBy)),
		CreatedAt: tsToTime(row.CreatedAt),
	}
}
