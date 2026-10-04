package pgdialect_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGovernedExecutionPreservesClassifierMeaningAcrossServerStringModes(t *testing.T) {
	pool, target, credential := freshExec(t)
	ctx := context.Background()
	var serverVersion int
	if err := pool.QueryRow(ctx, "SELECT current_setting('server_version_num')::integer").Scan(&serverVersion); err != nil {
		t.Fatal(err)
	}
	_, err := pool.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{target.DatabaseName}.Sanitize()+" SET standard_conforming_strings=off")
	expectedSetting := "off"
	if serverVersion >= 190000 {
		var unsupported *pgconn.PgError
		if !errors.As(err, &unsupported) || unsupported.Code != "0A000" {
			t.Fatalf("PG19 must refuse legacy strings with feature_not_supported, got %v", err)
		}
		expectedSetting = "on"
	} else if err != nil {
		t.Fatal(err)
	}
	legacy, err := pgxpool.NewWithConfig(ctx, pool.Config().Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	var setting string
	if err := legacy.QueryRow(ctx, "SHOW standard_conforming_strings").Scan(&setting); err != nil || setting != expectedSetting {
		t.Fatalf("fresh server string mode = %q, want %q: %v", setting, expectedSetting, err)
	}
	columns, rows, _, err := runExec(ctx, t, target, credential, query.Execution{
		SQL: `SELECT 'a\' -- ', 42 --'`, Class: query.ClassRead,
		MaxRows: 10, MaxResultBytes: 4096, TimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(columns) != 1 || len(rows) != 1 || len(rows[0]) != 1 || rows[0][0].Text != `a\` {
		t.Fatal("target interpreted approved SQL differently from the classifier")
	}
}
