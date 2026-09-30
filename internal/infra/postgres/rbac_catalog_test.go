package postgres_test

import (
	"context"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

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

	if len(catalog) != 35 {
		t.Fatalf("seeded catalog has %d keys, want 35", len(catalog))
	}
}
