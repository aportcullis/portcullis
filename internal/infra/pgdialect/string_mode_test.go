package pgdialect_test

import (
	"context"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGovernedExecutionPreservesClassifierMeaningWithLegacyStringDefaults(t *testing.T) {
	pool, target, credential := freshExec(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{target.DatabaseName}.Sanitize()+" SET standard_conforming_strings=off"); err != nil {
		t.Fatal(err)
	}
	legacy, err := pgxpool.NewWithConfig(ctx, pool.Config().Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	var setting string
	if err := legacy.QueryRow(ctx, "SHOW standard_conforming_strings").Scan(&setting); err != nil || setting != "off" {
		t.Fatal("fixture did not establish legacy server string interpretation")
	}
	columns, rows, _, err := runExec(ctx, t, target, credential, query.Execution{
		SQL: `SELECT 'a\' -- ', 42 --'`, Class: query.ClassRead, Governed: true,
		MaxRows: 10, MaxResultBytes: 4096, TimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(columns) != 1 || len(rows) != 1 || len(rows[0]) != 1 || rows[0][0].Text != `a\` {
		t.Fatal("target interpreted approved SQL differently from the classifier")
	}
}
