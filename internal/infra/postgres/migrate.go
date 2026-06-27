package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/migrations"
)

// Migrate applies every embedded migration not yet recorded, each in its own
// transaction, in lexical filename order. It is safe to call on every startup.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `create table if not exists schema_migrations (
		version text primary key,
		applied_at timestamptz not null default now())`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	names, err := migrationNames()
	if err != nil {
		return err
	}

	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		var applied bool
		if err := pool.QueryRow(ctx,
			`select exists(select 1 from schema_migrations where version = $1)`, version,
		).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}
		if err := applyOne(ctx, pool, version, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", version, err)
		}
	}
	return nil
}

func migrationNames() ([]string, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func applyOne(ctx context.Context, pool *pgxpool.Pool, version, body string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, body); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into schema_migrations (version) values ($1)`, version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
