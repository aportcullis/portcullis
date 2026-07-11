package postgres_test

import (
	"context"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// The seeded permission catalog (migration 0002) is the runtime source of truth
// loaded at startup (ADR-0008). Pin its size so a seed edit that drops or
// duplicates a key is caught, and prove the store satisfies authz.LoadCatalog.
func TestPermissionCatalogSeededSize(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	catalog, err := authz.LoadCatalog(ctx, pg.NewIdentityStore(pool))
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	// ADR-0008 appendix: 32 keys across users/roles/connections/policies/requests/
	// savedqueries/audit.
	if len(catalog) != 32 {
		t.Fatalf("seeded catalog has %d keys, want 32", len(catalog))
	}
}
