package dbtest_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/aportcullis/portcullis/internal/infra/dbtest"
)

// TestPostgresRunsRequestedFamily refuses evidence from a different server family.
func TestPostgresRunsRequestedFamily(t *testing.T) {
	settings, err := dbtest.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	want, err := strconv.Atoi(string(settings.PostgresFamily))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.TargetPostgres(t)
	var actual int
	if err := pool.QueryRow(context.Background(), "select current_setting('server_version_num')::int / 10000").Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != want {
		t.Fatalf("requested PostgreSQL %d; connected to %d", want, actual)
	}
	t.Logf("real PostgreSQL family %d verified", actual)
}

// TestMetadataPostgresStaysOnBaseline refuses target-family changes to metadata storage.
func TestMetadataPostgresStaysOnBaseline(t *testing.T) {
	pool := dbtest.Postgres(t)
	var actual int
	if err := pool.QueryRow(context.Background(), "select current_setting('server_version_num')::int / 10000").Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != 18 {
		t.Fatalf("metadata baseline is PostgreSQL 18; connected to %d", actual)
	}
}

// TestPostgresTestImageRejectsUnknownFamily refuses unreviewed container images.
func TestPostgresTestImageRejectsUnknownFamily(t *testing.T) {
	t.Setenv("PORTCULLIS_TEST_POSTGRES_FAMILY", "unreviewed")
	if _, err := dbtest.PostgresTestImage(); err == nil {
		t.Fatal("unreviewed target family accepted")
	}
}
