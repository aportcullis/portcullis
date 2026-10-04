package postgres_test

import (
	"context"
	"crypto/sha256"
	"io/fs"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/migrations"
)

// embeddedMigrationChecksums returns the sha256 of every embedded migration keyed by version.
func embeddedMigrationChecksums(t *testing.T) map[string][]byte {
	t.Helper()
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	checksums := make(map[string][]byte, len(names))
	for _, name := range names {
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(body)
		checksums[strings.TrimSuffix(name, ".sql")] = digest[:]
	}
	return checksums
}

func requireRecordedChecksums(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	want := embeddedMigrationChecksums(t)
	rows, err := pool.Query(context.Background(), `select version, checksum from public.schema_migrations`)
	if err != nil {
		t.Fatalf("read history checksums: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var version string
		var checksum []byte
		if err := rows.Scan(&version, &checksum); err != nil {
			t.Fatal(err)
		}
		if string(checksum) != string(want[version]) {
			t.Errorf("version %s checksum = %x, want %x", version, checksum, want[version])
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != len(want) {
		t.Errorf("history holds %d versions, want %d", seen, len(want))
	}
}

// applyLegacyHistoryBefore applies embedded migrations older than a version the way a pre-checksum binary did.
func applyLegacyHistoryBefore(t *testing.T, pool *pgxpool.Pool, before string) {
	t.Helper()
	ctx := context.Background()
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	for _, statement := range []string{
		`select set_config('search_path', 'public', false)`,
		`select set_config('portcullis.runtime_role', 'portcullis_runtime', false)`,
		`create table if not exists public.schema_migrations (version text primary key, applied_at timestamptz not null default now())`,
	} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		if version >= before {
			break
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, string(body)); err != nil {
			t.Fatalf("legacy apply %s: %v", version, err)
		}
		if _, err := conn.Exec(ctx, `insert into public.schema_migrations (version) values ($1)`, version); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrateRecordsAndBackfillsChecksums(t *testing.T) {
	ctx := context.Background()

	t.Run("a fresh database records every checksum", func(t *testing.T) {
		pool := dbtest.FreshPostgres(t)
		if err := pg.Migrate(ctx, pool); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		requireRecordedChecksums(t, pool)
	})

	t.Run("missing checksums are backfilled from the embedded files", func(t *testing.T) {
		pool := dbtest.FreshPostgres(t)
		if err := pg.Migrate(ctx, pool); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		if _, err := pool.Exec(ctx, `update public.schema_migrations set checksum = null`); err != nil {
			t.Fatalf("clear checksums: %v", err)
		}
		if err := pg.Migrate(ctx, pool); err != nil {
			t.Fatalf("Migrate after clearing: %v", err)
		}
		requireRecordedChecksums(t, pool)
	})

	t.Run("a pre-checksum history upgrades and backfills", func(t *testing.T) {
		pool := dbtest.FreshPostgres(t)
		applyLegacyHistoryBefore(t, pool, "0019")
		if err := pg.Migrate(ctx, pool); err != nil {
			t.Fatalf("Migrate legacy history: %v", err)
		}
		requireRecordedChecksums(t, pool)
	})

	t.Run("a partially applied legacy history applies the rest", func(t *testing.T) {
		pool := dbtest.FreshPostgres(t)
		applyLegacyHistoryBefore(t, pool, "0015")
		if err := pg.Migrate(ctx, pool); err != nil {
			t.Fatalf("Migrate partial history: %v", err)
		}
		requireRecordedChecksums(t, pool)
	})
}

func TestMigrateRefusesHistoryItCannotVouchFor(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, tamper, wantInError string
	}{
		{"an edited released migration", `update public.schema_migrations set checksum = sha256('edited') where version = '0007_connections'`, "0007_connections"},
		{"an edited latest migration", `update public.schema_migrations set checksum = sha256('edited') where version = (select max(version) from public.schema_migrations)`, "checksum"},
		{"a version from a newer binary", `insert into public.schema_migrations (version, checksum) values ('9999_from_the_future', sha256('future'))`, "9999_from_the_future"},
		{"an unknown version sorting first", `insert into public.schema_migrations (version) values ('0000_out_of_band_hotfix')`, "0000_out_of_band_hotfix"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.FreshPostgres(t)
			if err := pg.Migrate(ctx, pool); err != nil {
				t.Fatalf("Migrate: %v", err)
			}
			if _, err := pool.Exec(ctx, tc.tamper); err != nil {
				t.Fatalf("tamper: %v", err)
			}
			err := pg.Migrate(ctx, pool)
			if err == nil || !strings.Contains(err.Error(), tc.wantInError) {
				t.Fatalf("Migrate over %s = %v, want a refusal naming %q", tc.name, err, tc.wantInError)
			}
		})
	}

	t.Run("an unknown version blocks pending migrations", func(t *testing.T) {
		pool := dbtest.FreshPostgres(t)
		applyLegacyHistoryBefore(t, pool, "0019")
		if _, err := pool.Exec(ctx, `insert into public.schema_migrations (version) values ('9999_from_the_future')`); err != nil {
			t.Fatal(err)
		}
		if err := pg.Migrate(ctx, pool); err == nil {
			t.Fatal("Migrate with an unknown recorded version succeeded")
		}
		var pending bool
		if err := pool.QueryRow(ctx, `select not exists (select 1 from public.schema_migrations where version >= '0019' and version < '9999')`).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if !pending {
			t.Error("a refused history still applied pending migrations")
		}
	})
}

func TestMigrateBoundsTheMigrationLockWait(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.FreshPostgres(t)
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	holder, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	if _, err := holder.Exec(ctx, `select pg_advisory_lock(3, 0)`); err != nil {
		t.Fatalf("hold migration lock: %v", err)
	}

	started := time.Now()
	err = pg.Migrate(ctx, pool, pg.WithLockWaitTimeout(300*time.Millisecond))
	if err == nil || !strings.Contains(err.Error(), "migration lock") {
		t.Fatalf("Migrate behind a held lock = %v, want a bounded lock-wait refusal", err)
	}
	if waited := time.Since(started); waited > 5*time.Second {
		t.Errorf("lock wait took %s, want it bounded near 300ms", waited)
	}

	released := make(chan error, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, err := holder.Exec(ctx, `select pg_advisory_unlock(3, 0)`)
		released <- err
	}()
	if err := pg.Migrate(ctx, pool, pg.WithLockWaitTimeout(10*time.Second)); err != nil {
		t.Fatalf("Migrate once the lock is released = %v", err)
	}
	if err := <-released; err != nil {
		t.Fatal(err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := pg.Migrate(cancelled, pool, pg.WithLockWaitTimeout(time.Second)); err == nil {
		t.Error("Migrate with a cancelled context succeeded")
	}
	if err := pg.Migrate(ctx, pool, pg.WithLockWaitTimeout(0)); err == nil {
		t.Error("Migrate with a non-positive lock wait succeeded")
	}
}
