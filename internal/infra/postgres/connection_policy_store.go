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

// ConnectionPolicyStore implements the connection-policy app service's
// repository port over the sqlc queries (ADR-0015). Versions are append-only:
// an update bumps connections.current_policy_version optimistically and
// inserts the next version row in one transaction with its audit events
// (ADR-0009); the runtime role cannot UPDATE the version table at all.
type ConnectionPolicyStore struct {
	// conns is reused for org resolution, the withTx helper, and the
	// zero-rowcount disambiguation helpers — same pool, same queries.
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

// GetCurrent returns the connection's current policy snapshot. Archived
// connections keep answering — the policy is part of the historical snapshot.
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

// UpdatePolicy appends next as the new current version. The pointer bump is
// the optimistic guard: zero rows disambiguates into ErrNotFound / ErrArchived
// / ErrPolicyConflict; the (connection_id, version) PK and the deferred FK are
// the structural backstops. Audit events commit in the same transaction.
func (s *ConnectionPolicyStore) UpdatePolicy(ctx context.Context, next connection.Policy, expectedVersion int64, events ...audit.Event) (connection.Policy, error) {
	cid, oid, err := connIDs(next.ConnectionID, next.OrganizationID)
	if err != nil {
		return connection.Policy{}, err
	}
	params, err := policyInsertParams(next)
	if err != nil {
		return connection.Policy{}, err
	}
	err = s.conns.withTx(ctx, func(q *db.Queries) error {
		if _, err := q.BumpConnectionPolicyVersion(ctx, db.BumpConnectionPolicyVersionParams{
			ID:              cid,
			OrganizationID:  oid,
			ExpectedVersion: expectedVersion,
		}); err != nil {
			return s.missingArchivedOrPolicyConflict(ctx, q, cid, oid, err)
		}
		if err := q.InsertConnectionPolicyVersion(ctx, params); err != nil {
			return err
		}
		return insertEvents(ctx, q, events)
	})
	if err != nil {
		return connection.Policy{}, err
	}
	return next, nil
}

// missingArchivedOrPolicyConflict mirrors missingArchivedOrConflict but maps
// the "still exists, still active" case to ErrPolicyConflict — the policy
// pointer moved after the caller's read (a distinct message from a descriptor
// conflict).
func (s *ConnectionPolicyStore) missingArchivedOrPolicyConflict(ctx context.Context, q *db.Queries, id, org pgtype.UUID, err error) error {
	mapped := s.conns.missingArchivedOrConflict(ctx, q, id, org, err)
	if errors.Is(mapped, connection.ErrConflict) {
		return connection.ErrPolicyConflict
	}
	return mapped
}

// policyInsertParams converts a validated domain policy into the append-only
// insert row. Shared with ConnectionStore.Create, which inserts the v1 default
// alongside the connection.
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
		CreatedAt:              timeToTS(p.CreatedAt),
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
