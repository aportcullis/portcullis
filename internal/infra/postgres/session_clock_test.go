package postgres_test

import (
	"context"
	"crypto/sha256"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// sessionExpiriesFromDatabaseClock returns how far each stored expiry lies beyond the database's current clock.
func sessionExpiriesFromDatabaseClock(t *testing.T, pool *pgxpool.Pool, id identity.SessionID) (time.Duration, time.Duration) {
	t.Helper()
	var idleSeconds, absoluteSeconds float64
	if err := pool.QueryRow(context.Background(), `
		select extract(epoch from idle_expires_at - clock_timestamp()), extract(epoch from absolute_expires_at - clock_timestamp())
		from public.sessions where id = $1::uuid`, string(id)).Scan(&idleSeconds, &absoluteSeconds); err != nil {
		t.Fatalf("read session expiries: %v", err)
	}
	return time.Duration(idleSeconds * float64(time.Second)), time.Duration(absoluteSeconds * float64(time.Second))
}

func requireNear(t *testing.T, label string, got, want time.Duration) {
	t.Helper()
	if diff := got - want; diff < -10*time.Second || diff > 10*time.Second {
		t.Errorf("%s = %s past the database clock, want about %s", label, got, want)
	}
}

func TestSessionExpiriesAreAnchoredAtTheDatabaseClock(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)
	newUser := func(t *testing.T) identity.UserID {
		t.Helper()
		u, err := store.CreateUser(ctx, unique("session-clock")+"@example.com", "Session clock")
		if err != nil {
			t.Fatal(err)
		}
		return u.ID
	}
	token := func() []byte {
		digest := sha256.Sum256([]byte(unique("session-clock-token")))
		return digest[:]
	}

	for _, tc := range []struct {
		name string
		skew time.Duration
	}{
		{"an application clock an hour behind", -time.Hour},
		{"an application clock two hours ahead", 2 * time.Hour},
		{"an application clock a day behind", -24 * time.Hour},
	} {
		t.Run("create with "+tc.name, func(t *testing.T) {
			sess := identity.NewSession("", newUser(t), time.Now().Add(tc.skew), 30*time.Minute, 2*time.Hour)
			created, err := store.CreateSession(ctx, sess, token())
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			idle, absolute := sessionExpiriesFromDatabaseClock(t, pool, created.ID)
			requireNear(t, "idle expiry", idle, 30*time.Minute)
			requireNear(t, "absolute expiry", absolute, 2*time.Hour)
			if err := store.ValidateSession(ctx, created.ID); err != nil {
				t.Errorf("ValidateSession on a fresh session = %v", err)
			}
		})
	}

	t.Run("rotate with an application clock an hour behind", func(t *testing.T) {
		user := newUser(t)
		sess := identity.NewSession("", user, time.Now().Add(-time.Hour), 30*time.Minute, 2*time.Hour)
		rotated, err := store.RotateSession(ctx, user, sess, token(), testEvent(audit.ActionAuthLogin))
		if err != nil {
			t.Fatalf("RotateSession: %v", err)
		}
		idle, absolute := sessionExpiriesFromDatabaseClock(t, pool, rotated.ID)
		requireNear(t, "rotated idle expiry", idle, 30*time.Minute)
		requireNear(t, "rotated absolute expiry", absolute, 2*time.Hour)
	})

	t.Run("an idle slide extends from the database clock and never past absolute", func(t *testing.T) {
		sess := identity.NewSession("", newUser(t), time.Now(), 10*time.Minute, time.Hour)
		created, err := store.CreateSession(ctx, sess, token())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.ExtendSessionIdle(ctx, created.ID, time.Now().Add(45*time.Minute)); err != nil {
			t.Fatalf("ExtendSessionIdle: %v", err)
		}
		idle, _ := sessionExpiriesFromDatabaseClock(t, pool, created.ID)
		requireNear(t, "slid idle expiry", idle, 45*time.Minute)
		if err := store.ExtendSessionIdle(ctx, created.ID, time.Now().Add(-time.Minute)); err != nil {
			t.Fatalf("ExtendSessionIdle with an elapsed deadline: %v", err)
		}
		idle, _ = sessionExpiriesFromDatabaseClock(t, pool, created.ID)
		requireNear(t, "idle expiry after an elapsed deadline", idle, 45*time.Minute)
	})

	for _, tc := range []struct {
		name           string
		idle, absolute time.Duration
	}{
		{"a zero idle window", 0, time.Hour},
		{"a negative absolute window", 30 * time.Minute, -time.Minute},
	} {
		t.Run("create refuses "+tc.name, func(t *testing.T) {
			user := newUser(t)
			sess := identity.NewSession("", user, time.Now(), tc.idle, tc.absolute)
			if _, err := store.CreateSession(ctx, sess, token()); err == nil {
				t.Fatalf("CreateSession with %s succeeded", tc.name)
			}
			var stored int
			if err := pool.QueryRow(ctx, `select count(*) from public.sessions where user_id = $1::uuid`, string(user)).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if stored != 0 {
				t.Errorf("refused session left %d rows", stored)
			}
		})
	}
}

func TestSessionExpiryQueriesTakeNoApplicationTimestamps(t *testing.T) {
	body, err := os.ReadFile("queries/identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements := namedStatements(string(body))
	callerTimestamp := regexp.MustCompile(`(?i)(idle|absolute)_expires_at\s*(,|\))[^;]*values|sqlc\.arg\(\s*idle_expires_at\s*\)|\$3|\$4`)
	for _, name := range []string{"CreateSession", "ExtendSessionIdle"} {
		sql, ok := statements[name]
		if !ok {
			t.Fatalf("query %s is missing", name)
		}
		if callerTimestamp.MatchString(sql) {
			t.Errorf("%s takes a session expiry timestamp from the application; compute it from clock_timestamp() (ADR-0009)", name)
		}
		anchored := regexp.MustCompile(`(clock_timestamp\(\)|observed\.at)\s*\+\s*make_interval`)
		if !anchored.MatchString(sql) || !regexp.MustCompile(`clock_timestamp\(\)`).MatchString(sql) {
			t.Errorf("%s does not anchor its expiry at clock_timestamp() + make_interval", name)
		}
	}
}
