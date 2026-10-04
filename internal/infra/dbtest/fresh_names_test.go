package dbtest_test

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"testing"

	"github.com/aportcullis/portcullis/internal/infra/dbtest"
)

var freshDatabaseName = regexp.MustCompile(`^pc_fresh_[a-z0-9_]+$`)

func TestFreshPostgresNamesDoNotCollideAcrossTestBinaries(t *testing.T) {
	admin := dbtest.Postgres(t)
	ctx := context.Background()

	// Another test binary sharing this container has already created the legacy counter names this binary would otherwise reuse.
	legacy := make([]string, 0, 40)
	for idx := 1; idx <= 40; idx++ {
		name := fmt.Sprintf("pc_fresh_%d", idx)
		if _, err := admin.Exec(ctx, "create database "+name); err != nil {
			t.Logf("legacy name %s already present: %v", name, err)
			continue
		}
		legacy = append(legacy, name)
	}
	t.Cleanup(func() {
		for _, name := range legacy {
			_, _ = admin.Exec(context.Background(), "drop database if exists "+name+" with (force)")
		}
	})

	names := make(chan string, 8)
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			pool := dbtest.FreshPostgres(t)
			var name string
			if err := pool.QueryRow(ctx, "select current_database()").Scan(&name); err != nil {
				t.Error(err)
				return
			}
			names <- name
		})
	}
	wg.Wait()
	close(names)

	seen := map[string]bool{}
	for name := range names {
		if seen[name] {
			t.Errorf("fresh database name %s was issued twice", name)
		}
		seen[name] = true
		if !freshDatabaseName.MatchString(name) || len(name) > 63 {
			t.Errorf("fresh database name %q is not a plain identifier within 63 bytes", name)
		}
		if regexp.MustCompile(`^pc_fresh_\d+$`).MatchString(name) {
			t.Errorf("fresh database name %q uses the bare counter shape other binaries also produce", name)
		}
	}
	if len(seen) != 6 {
		t.Errorf("created %d distinct fresh databases, want 6", len(seen))
	}
}
