package postgres

import (
	"context"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ResultStore persists disposable encrypted snapshots behind owner-scoped access.
type ResultStore struct{ connections *ConnectionStore }

// NewResultStore builds the encrypted-result repository on the metadata pool.
func NewResultStore(pool *pgxpool.Pool) *ResultStore {
	return &ResultStore{connections: NewConnectionStore(pool)}
}

// ListOrganizationIDs enumerates the organizations whose result caches maintenance visits.
func (s *ResultStore) ListOrganizationIDs(ctx context.Context) ([]identity.OrganizationID, error) {
	return s.connections.ListOrganizationIDs(ctx)
}

// Put admits a bounded snapshot and records non-expired evictions atomically.
func (s *ResultStore) Put(ctx context.Context, result query.SealedResult) error {
	m := result.Metadata
	if m.ByteCount > query.MaxSnapshotBytes || m.ByteCount < 0 || m.RowCount < 0 || m.RowCount > 10000 || result.KeyVersion < 1 || len(result.Chunks) < 1 {
		return query.ErrResultStoreFull
	}
	id, org, owner, err := resultIDs(m.ID, m.OrganizationID, m.OwnerID)
	if err != nil {
		return err
	}
	chunks := make([]db.InsertResultChunksParams, 0, len(result.Chunks))
	for _, chunk := range result.Chunks {
		if chunk.Index < 0 || chunk.Index > maxResultChunkIndex {
			return query.ErrResultUnavailable
		}
		chunks = append(chunks, db.InsertResultChunksParams{ResultID: id, OrganizationID: org, ChunkIndex: int32(chunk.Index), Nonce: chunk.Nonce, Ciphertext: chunk.Ciphertext}) //nolint:gosec // bounded by maxResultChunkIndex above
	}
	var refused error
	err = s.connections.withTx(ctx, func(q *db.Queries) error {
		if err := q.LockResultAdmission(ctx, db.LockResultAdmissionParams{LockClass: lockClassResultAdmission, LockObject: organizationLockObject(m.OrganizationID)}); err != nil {
			return err
		}
		rows, err := q.ListResultAccounting(ctx, org)
		if err != nil {
			return err
		}
		at, err := observeInstant(ctx, q)
		if err != nil {
			return err
		}
		current := make([]query.CachedResult, len(rows))
		for idx, row := range rows {
			current[idx] = query.CachedResult{SnapshotMetadata: resultMetadata(row), LastAccessedAt: tsToTime(row.LastAccessedAt)}
		}
		evictions, admitErr := query.PlanResultAdmission(current, m, at, query.UserSnapshotQuota, query.GlobalSnapshotQuota)
		for _, eviction := range evictions {
			evictedID, err := stringToUUID(eviction.Result.ID)
			if err != nil {
				return err
			}
			if err := deleteResult(ctx, q, evictedID, org); err != nil {
				return err
			}
			if eviction.Cause != "expired" {
				evt := audit.Event{OrganizationID: m.OrganizationID, OccurredAt: at, ActorType: audit.ActorSystem, ActorService: "system:result-store", Action: audit.Action("RESULT_EVICTED"), TargetType: "result_set", TargetID: eviction.Result.ID, Outcome: audit.OutcomeSucceeded, Metadata: map[string]any{"cause": eviction.Cause}}
				if err := insertAuditTx(ctx, q, evt); err != nil {
					return err
				}
			}
		}
		if admitErr != nil {
			refused = admitErr
			return nil
		}
		// Start TTL at admission, after quota lock waits.
		if err := q.InsertResultSet(ctx, db.InsertResultSetParams{ID: id, OrganizationID: org, OwnerUserID: owner, RowCount: m.RowCount, ByteSize: m.ByteCount, Truncated: m.Truncated, At: timeToTS(at), ExpiresAt: timeToTS(at.Add(query.ResultTTL)), KeyVersion: int32(result.KeyVersion), WrappedDek: result.WrappedDEK}); err != nil {
			return err
		}
		// One COPY writes every chunk instead of a round trip per chunk while the admission lock is held.
		_, err = q.InsertResultChunks(ctx, chunks)
		return err
	})
	if err != nil {
		return err
	}
	return refused
}

// Get rechecks result ownership and TTL before returning sealed metadata.
func (s *ResultStore) Get(ctx context.Context, org identity.OrganizationID, owner identity.UserID, id string) (query.SealedResult, error) {
	rid, oid, uid, err := resultIDs(id, org, owner)
	if err != nil {
		return query.SealedResult{}, query.ErrResultUnavailable
	}
	row, err := s.connections.q.GetResultSet(ctx, db.GetResultSetParams{ID: rid, OrganizationID: oid, OwnerUserID: uid})
	if err != nil {
		return query.SealedResult{}, notFound(err, query.ErrResultUnavailable)
	}
	at, err := observeInstant(ctx, s.connections.q)
	if err != nil {
		return query.SealedResult{}, err
	}
	if err := s.connections.q.TouchResultSet(ctx, db.TouchResultSetParams{ID: rid, OrganizationID: oid, OwnerUserID: uid, At: timeToTS(at)}); err != nil {
		return query.SealedResult{}, err
	}
	return query.SealedResult{Metadata: resultMetadata(row), KeyVersion: uint32(row.KeyVersion), WrappedDEK: row.WrappedDek}, nil
}

// GetChunk returns one encrypted chunk only to the current owner.
func (s *ResultStore) GetChunk(ctx context.Context, org identity.OrganizationID, owner identity.UserID, id string, index int) (query.SealedResultChunk, error) {
	if index < 0 || index > maxResultChunkIndex {
		return query.SealedResultChunk{}, query.ErrResultUnavailable
	}
	rid, oid, uid, err := resultIDs(id, org, owner)
	if err != nil {
		return query.SealedResultChunk{}, query.ErrResultUnavailable
	}
	row, err := s.connections.q.GetResultChunk(ctx, db.GetResultChunkParams{ResultID: rid, OrganizationID: oid, OwnerUserID: uid, ChunkIndex: int32(index)})
	if err != nil {
		return query.SealedResultChunk{}, notFound(err, query.ErrResultUnavailable)
	}
	return query.SealedResultChunk{Index: index, Nonce: row.Nonce, Ciphertext: row.Ciphertext}, nil
}

// PurgeExpired deletes expired cache chunks and their accounting rows.
func (s *ResultStore) PurgeExpired(ctx context.Context, org identity.OrganizationID) error {
	oid, err := stringToUUID(string(org))
	if err != nil {
		return err
	}
	// Each batch is its own short transaction; rows held by an admission or reader are skipped until a later pass.
	for {
		purged := 0
		err := s.connections.withTx(ctx, func(q *db.Queries) error {
			expired, err := q.LockExpiredResults(ctx, db.LockExpiredResultsParams{OrganizationID: oid, BatchSize: resultPurgeBatchSize})
			if err != nil {
				return err
			}
			for _, id := range expired {
				if err := deleteResult(ctx, q, id, oid); err != nil {
					return err
				}
			}
			purged = len(expired)
			return nil
		})
		if err != nil || purged < resultPurgeBatchSize {
			return err
		}
	}
}

// organizationLockObject derives the per-organization advisory-lock object id.
func organizationLockObject(org identity.OrganizationID) int32 {
	return advisoryLockObject(string(org))
}

func resultIDs(id string, org identity.OrganizationID, owner identity.UserID) (pgtype.UUID, pgtype.UUID, pgtype.UUID, error) {
	rid, err := stringToUUID(id)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, err
	}
	oid, err := stringToUUID(string(org))
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, err
	}
	uid, err := stringToUUID(string(owner))
	return rid, oid, uid, err
}

func resultMetadata(row db.ResultCacheResultSet) query.SnapshotMetadata {
	return query.SnapshotMetadata{ID: uuidToString(row.ID), OrganizationID: identity.OrganizationID(uuidToString(row.OrganizationID)), OwnerID: identity.UserID(uuidToString(row.OwnerUserID)), RowCount: row.RowCount, ByteCount: row.ByteSize, Truncated: row.Truncated, CreatedAt: tsToTime(row.CreatedAt), ExpiresAt: tsToTime(row.ExpiresAt)}
}

func deleteResult(ctx context.Context, q *db.Queries, id, org pgtype.UUID) error {
	if err := q.DeleteResultChunks(ctx, db.DeleteResultChunksParams{ResultID: id, OrganizationID: org}); err != nil {
		return err
	}
	return q.DeleteResultSet(ctx, db.DeleteResultSetParams{ID: id, OrganizationID: org})
}
