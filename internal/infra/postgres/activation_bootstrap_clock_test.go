package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"hash/fnv"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBindingActivationBootstrapObservesTimeAfterPolicyLock(t *testing.T) {
	for _, scenario := range []string{"valid token after wait", "token expires while waiting"} {
		t.Run(scenario, func(t *testing.T) {
			pool := dbtest.FreshPostgres(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := pg.Migrate(ctx, pool); err != nil {
				t.Fatal(err)
			}
			store := pg.NewIdentityStore(pool)
			organization, err := store.DefaultOrganizationID(ctx)
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte("activation-bootstrap-token"))
			validity := time.Hour
			if scenario == "token expires while waiting" {
				validity = 2 * time.Second
			}
			if err := store.RotateSetupToken(ctx, hash[:], validity); err != nil {
				t.Fatal(err)
			}
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = blocker.Rollback(context.Background()) }()
			coordinator := fnv.New32a()
			_, _ = coordinator.Write([]byte(organization))
			object := int64(coordinator.Sum32())
			if object > 2147483647 {
				object -= 4294967296
			}
			if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock(5,$1::integer)`, object); err != nil {
				t.Fatal(err)
			}
			settings := pool.Config().Copy()
			settings.ConnConfig.RuntimeParams["application_name"] = "activation-bootstrap-clock"
			waiting, err := pgxpool.NewWithConfig(ctx, settings)
			if err != nil {
				t.Fatal(err)
			}
			defer waiting.Close()
			results := make(chan error, 1)
			go func() {
				_, err := pg.NewIdentityStore(waiting).BootstrapAdmin(ctx, "clock-admin@example.com", "Clock admin", "phc", hash[:], testEvent(audit.ActionAuthBootstrap))
				results <- err
			}()
			waitBindingPolicyLock(t, ctx, pool, "activation-bootstrap-clock")
			if scenario == "token expires while waiting" {
				if _, err := blocker.Exec(ctx, `select pg_catalog.pg_sleep(2.1)`); err != nil {
					t.Fatal(err)
				}
			}
			var releasedAt time.Time
			if err := blocker.QueryRow(ctx, `select clock_timestamp()`).Scan(&releasedAt); err != nil {
				t.Fatal(err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			err = <-results
			if scenario == "token expires while waiting" {
				if !errors.Is(err, identity.ErrSetupTokenInvalid) {
					t.Fatalf("expired bootstrap admitted after lock wait: %v", err)
				}
				var users int
				if err := pool.QueryRow(ctx, `select count(*) from public.users`).Scan(&users); err != nil || users != 0 {
					t.Fatalf("partial expired bootstrap: users=%d %v", users, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var createdAt, occurredAt time.Time
			if err := pool.QueryRow(ctx, `select account.created_at,event.occurred_at from public.users account join public.audit_events event on event.actor_user_id=account.id where event.action='AUTH_BOOTSTRAP'`).Scan(&createdAt, &occurredAt); err != nil {
				t.Fatal(err)
			}
			if createdAt.Before(releasedAt) || occurredAt.Before(releasedAt) {
				t.Fatalf("bootstrap timestamps predate policy wait: release=%s user=%s audit=%s", releasedAt, createdAt, occurredAt)
			}
		})
	}
}
