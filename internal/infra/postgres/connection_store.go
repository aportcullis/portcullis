package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// ConnectionStore implements the connection domain's repository port (and the connection app service's narrower view of it) over the sqlc queries. Every query is organization-scoped (ADR-0004); mutations commit their audit events in the same transaction (ADR-0009).
type ConnectionStore struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// NewConnectionStore builds the store on a connection pool.
func NewConnectionStore(pool *pgxpool.Pool) *ConnectionStore {
	return &ConnectionStore{pool: pool, q: db.New(pool)}
}

// DefaultOrganizationID resolves the single self-hosted organization (single-org MVP). Unlike the identity store's hot-path cache, connection operations are rare admin actions, so a plain query keeps this simple.
func (s *ConnectionStore) DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error) {
	org, err := s.q.GetDefaultOrganization(ctx)
	if err != nil {
		return "", err
	}
	return identity.OrganizationID(uuidToString(org.ID)), nil
}

// withReadSnapshot is readSnapshot (snapshot.go) over this store's pool.
func (s *ConnectionStore) withReadSnapshot(ctx context.Context, fn func(pgx.Tx, *db.Queries) error) error {
	return readSnapshot(ctx, s.pool, s.q, fn)
}

// withTx runs fn in a plain transaction (no advisory lock — the partial-unique index serializes the only cross-row invariant, the active display name).
func (s *ConnectionStore) withTx(ctx context.Context, fn func(*db.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful commit
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// lockAndObserve acquires the connection lock before reading the clock so timestamps cannot predate a lock wait (ADR-0009).
func (s *ConnectionStore) lockAndObserve(ctx context.Context, q *db.Queries, cid, oid pgtype.UUID) (time.Time, error) {
	if _, err := q.LockConnectionForWrite(ctx, db.LockConnectionForWriteParams{ID: cid, OrganizationID: oid}); err != nil {
		return time.Time{}, notFound(err, connection.ErrNotFound)
	}
	return observeInstant(ctx, q)
}

// Create inserts an active connection with its sealed credential and commits the given audit events with it.
func (s *ConnectionStore) Create(ctx context.Context, c connection.Connection, cred connection.SealedCredential, events ...audit.Event) error {
	id, org, err := connIDs(c.ID, c.OrganizationID)
	if err != nil {
		return err
	}
	createdBy, err := stringToUUID(string(c.CreatedBy))
	if err != nil {
		return fmt.Errorf("created_by: %w", err)
	}
	keyVersion := int32(cred.KeyVersion) //nolint:gosec // key versions count rotations, far below int32
	params := db.InsertConnectionParams{
		ID:                   id,
		OrganizationID:       org,
		DbType:               string(c.DBType),
		DisplayName:          c.DisplayName,
		Environment:          string(c.Environment),
		Description:          c.Description,
		Host:                 c.Target.Host,
		Port:                 int32(c.Target.Port),
		DatabaseName:         c.Target.DatabaseName,
		TlsMode:              string(c.TLSMode),
		TargetFingerprint:    c.Fingerprint,
		CredentialKeyVersion: &keyVersion,
		CredentialWrappedDek: cred.WrappedDEK,
		CredentialNonce:      cred.Nonce,
		CredentialCiphertext: cred.Ciphertext,
		CreatedBy:            createdBy,
		// At is filled inside the transaction below: c.CreatedAt is the domain object's timestamp (the application's clock), and the row's own timeline belongs to the database (ADR-0009).
	}
	// The v1 default policy is a storage-level companion of the insert (ADR-0015): InsertConnection points current_policy_version at 1 and the deferred FK checks the pair at commit, so a connection cannot exist without its policy row.
	policy := connection.DefaultPolicy()
	policy.ConnectionID = c.ID
	policy.OrganizationID = c.OrganizationID
	policy.CreatedBy = c.CreatedBy
	policy.CreatedAt = c.CreatedAt
	policyParams, err := policyInsertParams(policy)
	if err != nil {
		return err
	}
	err = s.withTx(ctx, func(q *db.Queries) error {
		// One instant for the connection row, its v1 policy, and the events — this insert takes no row lock (the row does not exist yet), so reading the clock here needs no preceding lock statement (ADR-0009).
		at, err := observeInstant(ctx, q)
		if err != nil {
			return err
		}
		params.At = timeToTS(at)
		policyParams.At = timeToTS(at)
		if err := q.InsertConnection(ctx, params); err != nil {
			return err
		}
		if err := q.InsertConnectionPolicyVersion(ctx, policyParams); err != nil {
			return err
		}
		return insertEvents(ctx, q, stampEvents(events, at))
	})
	return onUniqueViolation(err, connectionsOrgNameIndex, connection.ErrNameTaken)
}

// GetByID returns one connection within the organization scope.
func (s *ConnectionStore) GetByID(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (connection.Connection, error) {
	cid, oid, err := connIDs(id, org)
	if err != nil {
		return connection.Connection{}, err
	}
	row, err := s.q.GetConnection(ctx, db.GetConnectionParams{ID: cid, OrganizationID: oid})
	if err != nil {
		return connection.Connection{}, notFound(err, connection.ErrNotFound)
	}
	return toConnection(row), nil
}

// List returns the organization's connections, newest first.
func (s *ConnectionStore) List(ctx context.Context, org identity.OrganizationID, includeArchived bool) ([]connection.Connection, error) {
	oid, err := stringToUUID(string(org))
	if err != nil {
		return nil, fmt.Errorf("organization id: %w", err)
	}
	rows, err := s.q.ListConnections(ctx, db.ListConnectionsParams{OrganizationID: oid, IncludeArchived: includeArchived})
	if err != nil {
		return nil, err
	}
	out := make([]connection.Connection, len(rows))
	for idx, row := range rows {
		out[idx] = toConnection(row)
	}
	return out, nil
}

// UpdateDescriptor replaces descriptor fields, including archived history labels, only at expectedVersion; stale edits return ErrConflict.
func (s *ConnectionStore) UpdateDescriptor(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID, displayName string, env connection.Environment, description string, expectedVersion int64, events ...audit.Event) (connection.Connection, error) {
	cid, oid, err := connIDs(id, org)
	if err != nil {
		return connection.Connection{}, err
	}
	var row db.Connection
	err = s.withTx(ctx, func(q *db.Queries) error {
		at, err := s.lockAndObserve(ctx, q, cid, oid)
		if err != nil {
			return err
		}
		row, err = q.UpdateConnectionDescriptor(ctx, db.UpdateConnectionDescriptorParams{
			ID:              cid,
			OrganizationID:  oid,
			DisplayName:     displayName,
			Environment:     string(env),
			Description:     description,
			At:              timeToTS(at),
			ExpectedVersion: expectedVersion,
		})
		if err != nil {
			return s.missingOrConflict(ctx, q, cid, oid, err)
		}
		return insertEvents(ctx, q, stampEvents(events, at))
	})
	if err != nil {
		return connection.Connection{}, onUniqueViolation(err, connectionsOrgNameIndex, connection.ErrNameTaken)
	}
	return toConnection(row), nil
}

// ReplaceConfig atomically replaces the target and credential at expectedVersion; archived targets and stale edits are refused.
func (s *ConnectionStore) ReplaceConfig(ctx context.Context, c connection.Connection, expectedVersion int64, cred connection.SealedCredential, events ...audit.Event) (connection.Connection, error) {
	cid, oid, err := connIDs(c.ID, c.OrganizationID)
	if err != nil {
		return connection.Connection{}, err
	}
	keyVersion := int32(cred.KeyVersion) //nolint:gosec // key versions count rotations, far below int32
	params := db.ReplaceConnectionConfigParams{
		ID:                   cid,
		OrganizationID:       oid,
		DisplayName:          c.DisplayName,
		Environment:          string(c.Environment),
		Description:          c.Description,
		Host:                 c.Target.Host,
		Port:                 int32(c.Target.Port),
		DatabaseName:         c.Target.DatabaseName,
		TlsMode:              string(c.TLSMode),
		TargetFingerprint:    c.Fingerprint,
		CredentialKeyVersion: &keyVersion,
		CredentialWrappedDek: cred.WrappedDEK,
		CredentialNonce:      cred.Nonce,
		CredentialCiphertext: cred.Ciphertext,
		ExpectedVersion:      expectedVersion,
	}
	var row db.Connection
	err = s.withTx(ctx, func(q *db.Queries) error {
		if _, err := q.LockConnectionForWrite(ctx, db.LockConnectionForWriteParams{ID: cid, OrganizationID: oid}); err != nil {
			return notFound(err, connection.ErrNotFound)
		}
		// The sweep below touches this connection's live requests, which the connection lock does not cover — same rule as the archive cascade (ADR-0009).
		at, err := observeConfigChangeInstant(ctx, q, cid, oid)
		if err != nil {
			return err
		}
		params.At = timeToTS(at)
		row, err = q.ReplaceConnectionConfig(ctx, params)
		if err != nil {
			return s.missingArchivedOrConflict(ctx, q, cid, oid, err)
		}
		events = stampEvents(events, at)
		// Replacing the configuration replaces the target the approvers looked at, so the approvals given for the old one go with it (§4.3's "a changed connection needs a new request", ADR-0018). Same transaction, so no window exists where the new target carries an old approval.
		correlate := audit.Event{}
		if len(events) > 0 {
			correlate = events[0]
		}
		if err := expireRequestsForConfigChange(ctx, q, cid, oid, at, correlate); err != nil {
			return err
		}
		return insertEvents(ctx, q, events)
	})
	if err != nil {
		return connection.Connection{}, onUniqueViolation(err, connectionsOrgNameIndex, connection.ErrNameTaken)
	}
	return toConnection(row), nil
}

// Archive soft-deletes and nulls the credential columns in ONE statement (PRD §4.3), committing the audit events with it. Archiving an archived row fails with ErrAlreadyArchived.
func (s *ConnectionStore) Archive(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID, events ...audit.Event) (connection.Connection, error) {
	cid, oid, err := connIDs(id, org)
	if err != nil {
		return connection.Connection{}, err
	}
	var row db.Connection
	err = s.withTx(ctx, func(q *db.Queries) error {
		if _, err := q.LockConnectionForWrite(ctx, db.LockConnectionForWriteParams{ID: cid, OrganizationID: oid}); err != nil {
			return notFound(err, connection.ErrNotFound)
		}
		// §4.3: archive refuses while an execution is in flight. The state is unreachable until the execution slice; the guard is inherited (ADR-0018).
		executing, err := q.ExistsExecutingForConnection(ctx, db.ExistsExecutingForConnectionParams{ConnectionID: cid, OrganizationID: oid})
		if err != nil {
			return err
		}
		if executing {
			return connection.ErrExecutionInFlight
		}
		// The cascade's variant: it also locks the request rows it is about to sweep, which this transaction's connection lock does not cover.
		at, err := observeCascadeInstant(ctx, q, cid, oid)
		if err != nil {
			return err
		}
		row, err = q.ArchiveConnection(ctx, db.ArchiveConnectionParams{ID: cid, OrganizationID: oid, At: timeToTS(at)})
		if err != nil {
			if mapped := s.missingOrArchived(ctx, q, cid, oid, err); errors.Is(mapped, connection.ErrArchived) {
				return connection.ErrAlreadyArchived
			} else { //nolint:revive // keep the mapped error's two meanings adjacent
				return mapped
			}
		}
		events = completeArchiveEvents(events, toConnection(row))
		// The admin's own event carries the same instant as the rows and the derived events: one transaction, one moment (ADR-0009).
		events = stampEvents(events, at)
		// Sweep the connection's requests in the SAME transaction (§4.3, ADR-0018): drafts cancel, pending/approved expire, each with a derived system event correlated to the admin's archive event.
		correlate := audit.Event{}
		if len(events) > 0 {
			correlate = events[0]
		}
		if err := sweepRequestsForArchive(ctx, q, cid, oid, at, correlate); err != nil {
			return err
		}
		return insertEvents(ctx, q, events)
	})
	if err != nil {
		return connection.Connection{}, err
	}
	return toConnection(row), nil
}

// completeArchiveEvents derives archive evidence from the UPDATE ... RETURNING row while its transaction is still open. This makes the event self-contained and immune to a concurrent descriptor update between application lookup and archive (PRD §4.3/§6.1).
func completeArchiveEvents(events []audit.Event, c connection.Connection) []audit.Event {
	for idx := range events {
		if events[idx].Action != audit.ActionConnectionArchived {
			continue
		}
		events[idx].TargetID = string(c.ID)
		events[idx].Metadata = map[string]any{
			"display_name": c.DisplayName,
			"db_type":      string(c.DBType),
			"fingerprint":  c.Fingerprint,
		}
	}
	return events
}

// TestMaterial reads target and credential together to avoid mixing config versions; archived connections are refused.
func (s *ConnectionStore) TestMaterial(ctx context.Context, org identity.OrganizationID, id connection.ConnectionID) (connection.Connection, connection.SealedCredential, error) {
	cid, oid, err := connIDs(id, org)
	if err != nil {
		return connection.Connection{}, connection.SealedCredential{}, err
	}
	row, err := s.q.GetConnection(ctx, db.GetConnectionParams{ID: cid, OrganizationID: oid})
	if err != nil {
		return connection.Connection{}, connection.SealedCredential{}, notFound(err, connection.ErrNotFound)
	}
	if row.ArchivedAt.Valid || row.CredentialKeyVersion == nil {
		return connection.Connection{}, connection.SealedCredential{}, connection.ErrArchived
	}
	sealed := connection.SealedCredential{
		KeyVersion: uint32(*row.CredentialKeyVersion), //nolint:gosec // checked > 0 by the table constraint
		WrappedDEK: row.CredentialWrappedDek,
		Nonce:      row.CredentialNonce,
		Ciphertext: row.CredentialCiphertext,
	}
	return toConnection(row), sealed, nil
}

// missingOrArchived disambiguates a zero-rowcount active-only UPDATE: the row is either absent (ErrNotFound) or archived (ErrArchived). Non-ErrNoRows errors pass through.
func (s *ConnectionStore) missingOrArchived(ctx context.Context, q *db.Queries, id, org pgtype.UUID, err error) error {
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, getErr := q.GetConnection(ctx, db.GetConnectionParams{ID: id, OrganizationID: org}); getErr != nil {
		return notFound(getErr, connection.ErrNotFound)
	}
	return connection.ErrArchived
}

// Disambiguate failed optimistic writes by rereading the row. Descriptor updates permit archived rows; config updates do not.
func (s *ConnectionStore) missingOrConflict(ctx context.Context, q *db.Queries, id, org pgtype.UUID, err error) error {
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, getErr := q.GetConnection(ctx, db.GetConnectionParams{ID: id, OrganizationID: org}); getErr != nil {
		return notFound(getErr, connection.ErrNotFound)
	}
	return connection.ErrConflict
}

func (s *ConnectionStore) missingArchivedOrConflict(ctx context.Context, q *db.Queries, id, org pgtype.UUID, err error) error {
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	row, getErr := q.GetConnection(ctx, db.GetConnectionParams{ID: id, OrganizationID: org})
	if getErr != nil {
		return notFound(getErr, connection.ErrNotFound)
	}
	if row.ArchivedAt.Valid {
		return connection.ErrArchived
	}
	return connection.ErrConflict
}

// insertEvents writes the mutation's audit events on the transaction-bound queries so they commit with the change (ADR-0009).
func insertEvents(ctx context.Context, q *db.Queries, events []audit.Event) error {
	for _, evt := range events {
		if err := insertAuditTx(ctx, q, evt); err != nil {
			return err
		}
	}
	return nil
}

func connIDs(id connection.ConnectionID, org identity.OrganizationID) (pgtype.UUID, pgtype.UUID, error) {
	cid, err := stringToUUID(string(id))
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, fmt.Errorf("connection id: %w", err)
	}
	oid, err := stringToUUID(string(org))
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, fmt.Errorf("organization id: %w", err)
	}
	return cid, oid, nil
}

func toConnection(row db.Connection) connection.Connection {
	return connection.Connection{
		ID:             connection.ConnectionID(uuidToString(row.ID)),
		OrganizationID: identity.OrganizationID(uuidToString(row.OrganizationID)),
		DBType:         connection.DBType(row.DbType),
		DisplayName:    row.DisplayName,
		Environment:    connection.Environment(row.Environment),
		Description:    row.Description,
		Target: connection.Target{
			Host:         row.Host,
			Port:         uint16(row.Port), //nolint:gosec // checked 1..65535 by the table constraint
			DatabaseName: row.DatabaseName,
		},
		TLSMode:       connection.TLSMode(row.TlsMode),
		Fingerprint:   row.TargetFingerprint,
		CreatedBy:     identity.UserID(uuidToString(row.CreatedBy)),
		CreatedAt:     tsToTime(row.CreatedAt),
		UpdatedAt:     tsToTime(row.UpdatedAt),
		Version:       row.Version,
		ConfigVersion: row.ConfigVersion,
		ArchivedAt:    tsToTimePtr(row.ArchivedAt),
	}
}
