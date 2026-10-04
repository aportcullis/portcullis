package postgres_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
)

type resultAdmissionFixture struct {
	pool       *pgxpool.Pool
	store      *postgres.ResultStore
	defaultOrg identity.OrganizationID
	orgB       identity.OrganizationID
	owner      identity.UserID
}

func newResultAdmissionFixture(t *testing.T) resultAdmissionFixture {
	t.Helper()
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	identities := postgres.NewIdentityStore(pool)
	defaultOrg, err := identities.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var orgB identity.OrganizationID
	if err := pool.QueryRow(ctx, `insert into organizations (slug, name) values ($1, 'Org B') returning id::text`, uuid.NewString()).Scan(&orgB); err != nil {
		t.Fatal(err)
	}
	owner, err := identities.CreateUser(ctx, uuid.NewString()+"@example.com", "Admission owner")
	if err != nil {
		t.Fatal(err)
	}
	return resultAdmissionFixture{pool: pool, store: postgres.NewResultStore(pool), defaultOrg: defaultOrg, orgB: orgB, owner: owner.ID}
}

// sealedResultWithChunks builds a result with the given chunk count and logical size.
func sealedResultWithChunks(org identity.OrganizationID, owner identity.UserID, chunks int, bytes int64) query.SealedResult {
	sealed := query.SealedResult{
		Metadata:   query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: org, OwnerID: owner, ByteCount: bytes},
		KeyVersion: 1, WrappedDEK: []byte("admission-envelope"),
	}
	for idx := range chunks {
		sealed.Chunks = append(sealed.Chunks, query.SealedResultChunk{Index: idx, Nonce: []byte("nonce-123456"), Ciphertext: []byte("chunk-ciphertext")})
	}
	return sealed
}

// lockResultRow holds a row lock on one result until the returned transaction ends.
func lockResultRow(t *testing.T, pool *pgxpool.Pool, id string) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `select 1 from result_cache.result_sets where id = $1 for update`, id); err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestResultAdmissionIsSerializedPerOrganization(t *testing.T) {
	f := newResultAdmissionFixture(t)
	ctx := context.Background()

	// An expired org A result the next org A admission must delete; holding its row lock parks that admission mid-transaction.
	blocker := admitResultFixture(t, f.pool, f.store, f.defaultOrg, f.owner, true)
	holder := lockResultRow(t, f.pool, blocker)
	parked := make(chan error, 1)
	go func() { parked <- f.store.Put(ctx, sealedResultWithChunks(f.defaultOrg, f.owner, 1, 64)) }()
	time.Sleep(300 * time.Millisecond)

	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := f.store.Put(bounded, sealedResultWithChunks(f.orgB, f.owner, 1, 64)); err != nil {
		t.Errorf("org B admission while org A's admission is parked = %v, want it to proceed", err)
	}
	select {
	case err := <-parked:
		t.Fatalf("org A admission finished while its expired row was locked: %v", err)
	default:
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-parked; err != nil {
		t.Fatalf("parked org A admission after release = %v", err)
	}
	if resultRemains(t, f.pool, blocker) {
		t.Error("org A admission did not delete its expired result")
	}

	// Purge skips a row another transaction holds instead of waiting for it, and still removes the rest.
	// Admit all three live first: an admission deletes its organization's expired rows, so expiry is backdated afterwards.
	heldExpired := admitResultFixture(t, f.pool, f.store, f.orgB, f.owner, false)
	freeExpired := admitResultFixture(t, f.pool, f.store, f.orgB, f.owner, false)
	live := admitResultFixture(t, f.pool, f.store, f.orgB, f.owner, false)
	if _, err := f.pool.Exec(ctx, `update result_cache.result_sets set expires_at = clock_timestamp() - interval '1 minute' where id = any($1::uuid[])`, []string{heldExpired, freeExpired}); err != nil {
		t.Fatal(err)
	}
	purgeHolder := lockResultRow(t, f.pool, heldExpired)
	purgeCtx, cancelPurge := context.WithTimeout(ctx, 3*time.Second)
	defer cancelPurge()
	if err := f.store.PurgeExpired(purgeCtx, f.orgB); err != nil {
		t.Errorf("purge beside a locked expired row = %v, want it to skip the row", err)
	}
	if resultRemains(t, f.pool, freeExpired) {
		t.Error("purge left an unlocked expired result")
	}
	if !resultRemains(t, f.pool, heldExpired) || !resultRemains(t, f.pool, live) {
		t.Error("purge removed a locked expired result or a live one")
	}
	if err := purgeHolder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.store.PurgeExpired(ctx, f.orgB); err != nil {
		t.Fatal(err)
	}
	if resultRemains(t, f.pool, heldExpired) {
		t.Error("a later purge did not remove the released expired result")
	}

	cancelledCtx, cancelNow := context.WithCancel(ctx)
	cancelNow()
	if err := f.store.Put(cancelledCtx, sealedResultWithChunks(f.orgB, f.owner, 1, 64)); err == nil {
		t.Error("Put with a cancelled context succeeded")
	}
}

func TestResultAdmissionWritesChunksInOneBatch(t *testing.T) {
	f := newResultAdmissionFixture(t)
	ctx := context.Background()
	var statements atomic.Int64
	cfg, err := pgxpool.ParseConfig(f.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = queryTracer{before: func(sql string) {
		if strings.Contains(sql, "result_chunks") {
			statements.Add(1)
		}
	}}
	traced, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(traced.Close)
	store := postgres.NewResultStore(traced)

	for _, chunks := range []int{1, 2, 37, 101} {
		statements.Store(0)
		sealed := sealedResultWithChunks(f.defaultOrg, f.owner, chunks, 64)
		if err := store.Put(ctx, sealed); err != nil {
			t.Fatalf("Put with %d chunks: %v", chunks, err)
		}
		if got := statements.Load(); got > 1 {
			t.Errorf("Put with %d chunks issued %d chunk statements, want the chunks written in one batch", chunks, got)
		}
		var stored int
		if err := f.pool.QueryRow(ctx, `select count(*) from result_cache.result_chunks where result_id = $1`, sealed.Metadata.ID).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored != chunks {
			t.Errorf("stored %d chunks, want %d", stored, chunks)
		}
		last, err := store.GetChunk(ctx, f.defaultOrg, f.owner, sealed.Metadata.ID, chunks-1)
		if err != nil || last.Index != chunks-1 {
			t.Errorf("GetChunk(%d) = %+v, %v", chunks-1, last, err)
		}
	}

	for _, tc := range []struct {
		name   string
		mutate func(*query.SealedResult)
	}{
		{"a chunk index past the cap", func(sealed *query.SealedResult) { sealed.Chunks[1].Index = 101 }},
		{"a negative chunk index", func(sealed *query.SealedResult) { sealed.Chunks[1].Index = -1 }},
		{"a duplicate chunk index", func(sealed *query.SealedResult) { sealed.Chunks[1].Index = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sealed := sealedResultWithChunks(f.defaultOrg, f.owner, 3, 64)
			tc.mutate(&sealed)
			if err := store.Put(ctx, sealed); err == nil {
				t.Fatalf("Put with %s succeeded", tc.name)
			}
			if resultRemains(t, f.pool, sealed.Metadata.ID) {
				t.Errorf("Put with %s left its result set behind", tc.name)
			}
		})
	}
}

func TestResultAdmissionAppliesPlannedEvictionsOnlyOnSuccess(t *testing.T) {
	f := newResultAdmissionFixture(t)
	ctx := context.Background()
	oldest := sealedResultWithChunks(f.defaultOrg, f.owner, 1, query.MaxSnapshotBytes)
	if err := f.store.Put(ctx, oldest); err != nil {
		t.Fatal(err)
	}
	newer := sealedResultWithChunks(f.defaultOrg, f.owner, 1, query.MaxSnapshotBytes)
	if err := f.store.Put(ctx, newer); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `update result_cache.result_sets set last_accessed_at = last_accessed_at - interval '1 hour' where id = $1`, oldest.Metadata.ID); err != nil {
		t.Fatal(err)
	}
	incoming := sealedResultWithChunks(f.defaultOrg, f.owner, 1, query.MaxSnapshotBytes)
	if err := f.store.Put(ctx, incoming); err != nil {
		t.Fatalf("Put past the user quota: %v", err)
	}
	if resultRemains(t, f.pool, oldest.Metadata.ID) {
		t.Error("the least recently used own result survived a user-quota admission")
	}
	if !resultRemains(t, f.pool, newer.Metadata.ID) || !resultRemains(t, f.pool, incoming.Metadata.ID) {
		t.Error("admission evicted more than it needed or dropped the incoming result")
	}
	var evicted int
	if err := f.pool.QueryRow(ctx, `select count(*) from audit_events where action = 'RESULT_EVICTED' and target_id = $1 and organization_id = $2`, oldest.Metadata.ID, string(f.defaultOrg)).Scan(&evicted); err != nil {
		t.Fatal(err)
	}
	if evicted != 1 {
		t.Errorf("RESULT_EVICTED events for the evicted result = %d, want 1", evicted)
	}
	if err := f.store.Put(ctx, sealedResultWithChunks(f.defaultOrg, f.owner, 1, query.MaxSnapshotBytes+1)); !errors.Is(err, query.ErrResultStoreFull) {
		t.Errorf("an oversized result = %v, want ErrResultStoreFull", err)
	}
}
