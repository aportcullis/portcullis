package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestForeignKeysHaveCoveringIndexes(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// A foreign key is covered when a non-partial index leads with its first column, so referenced-row deletes and joins never scan the referencing table (data.md: an index for every foreign key). A partial index does not count: it misses the rows outside its predicate.
	rows, err := pool.Query(ctx, `
		select c.conrelid::regclass::text, c.conname
		from pg_constraint c
		join pg_namespace n on n.oid = c.connamespace
		where c.contype = 'f' and n.nspname in ('public', 'result_cache')
		  and not exists (
		      select 1 from pg_index i
		      where i.indrelid = c.conrelid and i.indpred is null and i.indkey[0] = c.conkey[1])
		order by 1, 2`)
	if err != nil {
		t.Fatalf("inspect foreign keys: %v", err)
	}
	defer rows.Close()
	var uncovered []string
	for rows.Next() {
		var table, constraint string
		if err := rows.Scan(&table, &constraint); err != nil {
			t.Fatal(err)
		}
		uncovered = append(uncovered, table+"."+constraint)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(uncovered) > 0 {
		t.Errorf("foreign keys without a covering index: %s", strings.Join(uncovered, ", "))
	}
}

func TestLookupIndexesServeTheirQueries(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, tc := range []struct {
		index, wantDefinition string
	}{
		{"access_requests_org_requester_created_idx", "(organization_id, requester_id, created_at DESC, id DESC)"},
		{"audit_events_actor_user_idx", "(actor_user_id)"},
		{"access_requests_connection_org_idx", "(connection_id, organization_id)"},
		{"sessions_active_user_idx", "(user_id) WHERE (revoked_at IS NULL)"},
		{"sessions_active_idle_idx", "(idle_expires_at) WHERE (revoked_at IS NULL)"},
	} {
		var definition string
		err := pool.QueryRow(ctx, `select indexdef from pg_indexes where schemaname = 'public' and indexname = $1`, tc.index).Scan(&definition)
		if err != nil {
			t.Errorf("index %s: %v", tc.index, err)
			continue
		}
		if !strings.Contains(definition, tc.wantDefinition) {
			t.Errorf("index %s = %s, want it to contain %s", tc.index, definition, tc.wantDefinition)
		}
	}
}
