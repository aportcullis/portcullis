// Package dbtest shares container-backed databases per test binary; the testcontainers reaper removes them on exit.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/aportcullis/portcullis/internal/platform/logging"
)

// PostgresImage pins the disposable database used by Go and browser integration tests.
const PostgresImage = "postgres:18.6-alpine3.24@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873"

type postgresFixture struct {
	once     sync.Once
	pool     *pgxpool.Pool
	dsn      string
	err      error
	required bool
	freshDB  atomic.Int64
}

var metadataPostgres, targetPostgres postgresFixture

// freshDatabaseProcessToken identifies this test binary's fresh databases: its PID plus random bytes, so a reused PID on another host or container cannot collide either.
var freshDatabaseProcessToken = func() string {
	random := make([]byte, 6)
	_, _ = rand.Read(random)
	return fmt.Sprintf("%d_%s", os.Getpid(), hex.EncodeToString(random))
}()

// Postgres returns the PostgreSQL 18 metadata fixture, independently of target-family selection.
func Postgres(t testing.TB) *pgxpool.Pool {
	t.Helper()
	settings, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return metadataPostgres.connect(t, settings.MetadataURL, PostgresImage, settings.Required)
}

// TargetPostgres returns the managed-target database selected for compatibility tests.
func TargetPostgres(t testing.TB) *pgxpool.Pool {
	t.Helper()
	settings, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	image, err := imageForPostgresFamily(settings.PostgresFamily)
	if err != nil {
		t.Fatal(err)
	}
	return targetPostgres.connect(t, settings.TargetURL, image, settings.Required)
}

func (f *postgresFixture) connect(t testing.TB, dsn, image string, required bool) *pgxpool.Pool {
	t.Helper()
	f.once.Do(func() { f.required = required; f.start(dsn, image) })
	if f.err != nil {
		if f.required {
			t.Fatalf("required postgres unavailable: %v", f.err)
		}
		t.Skipf("postgres unavailable: %v", f.err)
	}
	return f.pool
}

func (f *postgresFixture) start(dsn, image string) {
	ctx := context.Background()
	if dsn == "" {
		tcLogger := logging.NewPrintfLogger(logging.New("debug", "json"), slog.LevelDebug, "testcontainers")
		container, err := tcpostgres.Run(ctx, image,
			tcpostgres.WithDatabase("portcullis"),
			tcpostgres.WithUsername("portcullis"),
			tcpostgres.WithPassword("portcullis"),
			testcontainers.WithLogger(tcLogger),
			// Wait for both entrypoint server phases before accepting connections.
			tcpostgres.BasicWaitStrategies(),
		)
		if err != nil {
			f.err = err
			return
		}
		if dsn, err = container.ConnectionString(ctx, "sslmode=disable"); err != nil {
			f.err = err
			return
		}
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		f.err = err
		return
	}
	f.pool, f.dsn = pool, dsn
}

// FreshPostgres creates a brand-new, empty database in the shared container and returns a pool to it (migrations NOT applied — the caller migrates). Use it for tests that need global isolation, e.g. first-run bootstrap which asserts on the whole users table. The database is dropped at test end.
func FreshPostgres(t testing.TB) *pgxpool.Pool {
	t.Helper()
	admin := Postgres(t)
	return metadataPostgres.fresh(t, admin)
}

// FreshTargetPostgres creates a brand-new database on the selected managed-target family.
func FreshTargetPostgres(t testing.TB) *pgxpool.Pool {
	t.Helper()
	admin := TargetPostgres(t)
	return targetPostgres.fresh(t, admin)
}

func (f *postgresFixture) fresh(t testing.TB, admin *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	// The process token keeps names unique across test binaries sharing one container; the counter keeps them unique within this binary.
	name := fmt.Sprintf("pc_fresh_%s_%d", freshDatabaseProcessToken, f.freshDB.Add(1))
	if _, err := admin.Exec(ctx, "create database "+name); err != nil {
		if f.required {
			t.Fatalf("required fresh postgres could not create %s (CREATEDB privilege and a unique name are required): %v", name, err)
		}
		// An external test DB (PORTCULLIS_TEST_DATABASE_URL) may connect as a non-superuser without CREATEDB; skip rather than fail there.
		t.Skipf("FreshPostgres needs CREATEDB privilege: %v", err)
	}

	u, err := url.Parse(f.dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatalf("connect fresh db: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		// DROP needs no active connections to the target database.
		_, _ = admin.Exec(context.Background(), "drop database if exists "+name+" with (force)")
	})
	return pool
}
