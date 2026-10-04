package postgres_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	resultapp "github.com/aportcullis/portcullis/internal/app/result"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
)

// admitResultFixture stores one small sealed result for an owner in an organization and optionally backdates its expiry.
func admitResultFixture(t *testing.T, pool *pgxpool.Pool, store *postgres.ResultStore, org identity.OrganizationID, owner identity.UserID, expired bool) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	sealed := query.SealedResult{
		Metadata:   query.SnapshotMetadata{ID: id, OrganizationID: org, OwnerID: owner, ByteCount: 64},
		KeyVersion: 1, WrappedDEK: []byte("purge-fixture-envelope"),
		Chunks: []query.SealedResultChunk{{Index: 0, Nonce: []byte("nonce-123456"), Ciphertext: []byte("purge-fixture-ciphertext")}},
	}
	if err := store.Put(ctx, sealed); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if expired {
		if _, err := pool.Exec(ctx, `update result_cache.result_sets set expires_at = $1 where id = $2`, time.Now().Add(-time.Minute), id); err != nil {
			t.Fatalf("backdate: %v", err)
		}
	}
	return id
}

func resultRemains(t *testing.T, pool *pgxpool.Pool, id string) bool {
	t.Helper()
	var sets, chunks int
	if err := pool.QueryRow(context.Background(),
		`select (select count(*) from result_cache.result_sets where id = $1), (select count(*) from result_cache.result_chunks where result_id = $1)`, id,
	).Scan(&sets, &chunks); err != nil {
		t.Fatalf("count result rows: %v", err)
	}
	if (sets == 0) != (chunks == 0) {
		t.Fatalf("result %s has %d set rows but %d chunks", id, sets, chunks)
	}
	return sets > 0
}

func TestResultPurgeCoversEveryOrganization(t *testing.T) {
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
		t.Fatalf("create org B: %v", err)
	}
	owner, err := identities.CreateUser(ctx, uuid.NewString()+"@example.com", "Purge owner")
	if err != nil {
		t.Fatal(err)
	}
	store := postgres.NewResultStore(pool)

	organizations, err := store.ListOrganizationIDs(ctx)
	if err != nil {
		t.Fatalf("ListOrganizationIDs: %v", err)
	}
	if len(organizations) != 2 || !slices.Contains(organizations, defaultOrg) || !slices.Contains(organizations, orgB) {
		t.Fatalf("organizations = %v, want default %s and org B %s", organizations, defaultOrg, orgB)
	}
	requestOrganizations, err := postgres.NewAccessRequestStore(pool).ListOrganizationIDs(ctx)
	if err != nil || !slices.Equal(requestOrganizations, organizations) {
		t.Fatalf("request-store organizations = %v, %v; want %v", requestOrganizations, err, organizations)
	}

	defaultExpired := admitResultFixture(t, pool, store, defaultOrg, owner.ID, true)
	orgBExpired := admitResultFixture(t, pool, store, orgB, owner.ID, true)

	if err := store.PurgeExpired(ctx, orgB); err != nil {
		t.Fatalf("PurgeExpired org B: %v", err)
	}
	if resultRemains(t, pool, orgBExpired) {
		t.Error("org B purge kept its own expired result")
	}
	if !resultRemains(t, pool, defaultExpired) {
		t.Error("org B purge removed the default organization's result")
	}
	if err := store.PurgeExpired(ctx, identity.OrganizationID(uuid.NewString())); err != nil {
		t.Errorf("purge of an unknown organization = %v, want a no-op", err)
	}
	if !resultRemains(t, pool, defaultExpired) {
		t.Error("an unknown organization's purge removed another organization's result")
	}
	if err := store.PurgeExpired(ctx, "not-a-uuid"); err == nil {
		t.Error("purge with a malformed organization succeeded")
	}

	orgBExpiredAgain := admitResultFixture(t, pool, store, orgB, owner.ID, true)
	defaultLive := admitResultFixture(t, pool, store, defaultOrg, owner.ID, false)
	orgBLive := admitResultFixture(t, pool, store, orgB, owner.ID, false)
	if err := resultapp.PurgeExpiredInEveryOrganization(ctx, store); err != nil {
		t.Fatalf("PurgeExpiredInEveryOrganization: %v", err)
	}
	for _, expired := range []string{defaultExpired, orgBExpiredAgain} {
		if resultRemains(t, pool, expired) {
			t.Errorf("expired result %s survived the every-organization purge", expired)
		}
	}
	for _, live := range []string{defaultLive, orgBLive} {
		if !resultRemains(t, pool, live) {
			t.Errorf("live result %s was purged", live)
		}
	}
}
