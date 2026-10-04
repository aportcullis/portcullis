package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

const setupTokenTTL = 24 * time.Hour

func newSetupTokenStore(t *testing.T) (*pgxpool.Pool, *pg.IdentityStore) {
	t.Helper()
	pool := dbtest.FreshPostgres(t)
	if err := pg.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool, pg.NewIdentityStore(pool)
}

func setupTokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func countSetupTokens(t *testing.T, pool *pgxpool.Pool, predicate string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `select count(*) from setup_tokens where `+predicate).Scan(&count); err != nil {
		t.Fatalf("count setup tokens: %v", err)
	}
	return count
}

func TestSetupTokenStoreConsumesTheOutstandingToken(t *testing.T) {
	ctx := context.Background()

	t.Run("outstanding token is consumed with the admin", func(t *testing.T) {
		pool, store := newSetupTokenStore(t)
		if err := store.RotateSetupToken(ctx, setupTokenHash("first"), setupTokenTTL); err != nil {
			t.Fatalf("RotateSetupToken: %v", err)
		}
		if _, err := store.BootstrapAdmin(ctx, "admin@example.com", "Admin", "phc-hash", setupTokenHash("first"), testEvent(audit.ActionAuthBootstrap)); err != nil {
			t.Fatalf("BootstrapAdmin: %v", err)
		}
		if consumed := countSetupTokens(t, pool, "consumed_at is not null and deleted_at is null"); consumed != 1 {
			t.Errorf("consumed tokens = %d, want 1", consumed)
		}
	})

	t.Run("rotation soft-revokes the predecessor and keeps one outstanding", func(t *testing.T) {
		pool, store := newSetupTokenStore(t)
		for _, token := range []string{"first", "second", "third"} {
			if err := store.RotateSetupToken(ctx, setupTokenHash(token), setupTokenTTL); err != nil {
				t.Fatalf("RotateSetupToken(%s): %v", token, err)
			}
		}
		if outstanding := countSetupTokens(t, pool, "consumed_at is null and deleted_at is null"); outstanding != 1 {
			t.Errorf("outstanding tokens = %d, want 1", outstanding)
		}
		if revoked := countSetupTokens(t, pool, "deleted_at is not null"); revoked != 2 {
			t.Errorf("soft-revoked tokens = %d, want 2 retained rows", revoked)
		}
		if _, err := store.BootstrapAdmin(ctx, "admin@example.com", "Admin", "phc-hash", setupTokenHash("third"), testEvent(audit.ActionAuthBootstrap)); err != nil {
			t.Fatalf("BootstrapAdmin(latest) = %v", err)
		}
	})

	t.Run("only the hash is stored with a database-clock expiry", func(t *testing.T) {
		pool, store := newSetupTokenStore(t)
		if err := store.RotateSetupToken(ctx, setupTokenHash("raw-secret"), setupTokenTTL); err != nil {
			t.Fatalf("RotateSetupToken: %v", err)
		}
		var storedHash []byte
		var lifetime time.Duration
		if err := pool.QueryRow(ctx, `select token_hash, expires_at - created_at from setup_tokens`).Scan(&storedHash, &lifetime); err != nil {
			t.Fatalf("read setup token: %v", err)
		}
		if string(storedHash) != string(setupTokenHash("raw-secret")) || lifetime != setupTokenTTL {
			t.Errorf("stored token = %x lifetime %s, want the SHA-256 hash and %s", storedHash, lifetime, setupTokenTTL)
		}
	})

	t.Run("operator provisioning needs no token", func(t *testing.T) {
		_, store := newSetupTokenStore(t)
		if err := store.RotateSetupToken(ctx, setupTokenHash("unused"), setupTokenTTL); err != nil {
			t.Fatalf("RotateSetupToken: %v", err)
		}
		if _, err := store.BootstrapAdmin(ctx, "admin@example.com", "Admin", "phc-hash", nil, testEvent(audit.ActionAuthBootstrap)); err != nil {
			t.Fatalf("BootstrapAdmin(provisioned) = %v", err)
		}
	})
}

func TestSetupTokenStoreRefusesInvalidClaims(t *testing.T) {
	ctx := context.Background()

	refusals := []struct {
		name    string
		prepare func(t *testing.T, pool *pgxpool.Pool, store *pg.IdentityStore)
		claim   string
	}{
		{name: "wrong token", claim: "guess"},
		{name: "rotated-away token", claim: "first", prepare: func(t *testing.T, _ *pgxpool.Pool, store *pg.IdentityStore) {
			if err := store.RotateSetupToken(ctx, setupTokenHash("second"), setupTokenTTL); err != nil {
				t.Fatalf("RotateSetupToken: %v", err)
			}
		}},
		{name: "expired token", claim: "first", prepare: func(t *testing.T, pool *pgxpool.Pool, _ *pg.IdentityStore) {
			if _, err := pool.Exec(ctx, `update setup_tokens set created_at = now() - interval '2 days', expires_at = now() - interval '1 second'`); err != nil {
				t.Fatalf("expire setup token: %v", err)
			}
		}},
	}
	for _, refusal := range refusals {
		t.Run(refusal.name, func(t *testing.T) {
			pool, store := newSetupTokenStore(t)
			if err := store.RotateSetupToken(ctx, setupTokenHash("first"), setupTokenTTL); err != nil {
				t.Fatalf("RotateSetupToken: %v", err)
			}
			if refusal.prepare != nil {
				refusal.prepare(t, pool, store)
			}
			_, err := store.BootstrapAdmin(ctx, "admin@example.com", "Admin", "phc-hash", setupTokenHash(refusal.claim), testEvent(audit.ActionAuthBootstrap))
			if !errors.Is(err, identity.ErrSetupTokenInvalid) {
				t.Fatalf("BootstrapAdmin = %v, want ErrSetupTokenInvalid", err)
			}
			if users, _ := store.CountUsers(ctx); users != 0 {
				t.Errorf("refused claim left %d users", users)
			}
			if consumed := countSetupTokens(t, pool, "consumed_at is not null"); consumed != 0 {
				t.Errorf("refused claim consumed %d tokens", consumed)
			}
		})
	}

	t.Run("concurrent claims with one token admit exactly one admin", func(t *testing.T) {
		pool, store := newSetupTokenStore(t)
		if err := store.RotateSetupToken(ctx, setupTokenHash("race"), setupTokenTTL); err != nil {
			t.Fatalf("RotateSetupToken: %v", err)
		}
		const claimants = 8
		claimCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		var wait sync.WaitGroup
		results := make([]error, claimants)
		for idx := range claimants {
			wait.Go(func() {
				_, results[idx] = store.BootstrapAdmin(claimCtx, unique("racer")+"@example.com", "Admin", "phc-hash", setupTokenHash("race"), testEvent(audit.ActionAuthBootstrap))
			})
		}
		wait.Wait()
		admitted, refused := 0, 0
		for _, err := range results {
			switch {
			case err == nil:
				admitted++
			case errors.Is(err, identity.ErrAlreadyBootstrapped), errors.Is(err, identity.ErrSetupTokenInvalid):
				refused++
			default:
				t.Errorf("unexpected claim error: %v", err)
			}
		}
		if admitted != 1 || refused != claimants-1 {
			t.Errorf("admitted %d and refused %d, want 1 and %d", admitted, refused, claimants-1)
		}
		if users, _ := store.CountUsers(ctx); users != 1 {
			t.Errorf("users = %d, want 1", users)
		}
		if consumed := countSetupTokens(t, pool, "consumed_at is not null"); consumed != 1 {
			t.Errorf("consumed tokens = %d, want 1", consumed)
		}
	})

	t.Run("no token is issued once an admin exists", func(t *testing.T) {
		pool, store := newSetupTokenStore(t)
		if _, err := store.BootstrapAdmin(ctx, "admin@example.com", "Admin", "phc-hash", nil, testEvent(audit.ActionAuthBootstrap)); err != nil {
			t.Fatalf("BootstrapAdmin: %v", err)
		}
		if err := store.RotateSetupToken(ctx, setupTokenHash("late"), setupTokenTTL); !errors.Is(err, identity.ErrAlreadyBootstrapped) {
			t.Fatalf("RotateSetupToken after bootstrap = %v, want ErrAlreadyBootstrapped", err)
		}
		if stored := countSetupTokens(t, pool, "true"); stored != 0 {
			t.Errorf("setup tokens after refused rotation = %d, want 0", stored)
		}
	})
}
