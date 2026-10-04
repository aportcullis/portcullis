package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestCloseExpiredSessionsKeepsHistoryAndSkipsLiveRows(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)
	newSession := func(t *testing.T, label string) identity.Session {
		t.Helper()
		u, err := store.CreateUser(ctx, unique(label)+"@example.com", label)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(unique(label + "-token")))
		created, err := store.CreateSession(ctx, identity.NewSession("", u.ID, time.Now(), time.Hour, 24*time.Hour), digest[:])
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	setExpiry := func(t *testing.T, sess identity.Session, assignment string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `update public.sessions set `+assignment+` where id = $1::uuid`, string(sess.ID)); err != nil {
			t.Fatal(err)
		}
	}
	endedAt := func(t *testing.T, sess identity.Session) (*time.Time, time.Time, time.Time) {
		t.Helper()
		var revoked *time.Time
		var idle, absolute time.Time
		if err := pool.QueryRow(ctx, `select revoked_at, idle_expires_at, absolute_expires_at from public.sessions where id = $1::uuid`, string(sess.ID)).Scan(&revoked, &idle, &absolute); err != nil {
			t.Fatal(err)
		}
		return revoked, idle, absolute
	}

	idleExpired := newSession(t, "idle-expired")
	setExpiry(t, idleExpired, `idle_expires_at = clock_timestamp() - interval '10 minutes'`)
	absoluteExpired := newSession(t, "absolute-expired")
	setExpiry(t, absoluteExpired, `absolute_expires_at = clock_timestamp() - interval '5 minutes', idle_expires_at = clock_timestamp() + interval '1 hour'`)
	live := newSession(t, "live")
	revoked := newSession(t, "revoked")
	if err := store.RevokeSession(ctx, revoked.ID, testEvent(audit.ActionAuthLogout)); err != nil {
		t.Fatal(err)
	}
	originalRevocation, _, _ := endedAt(t, revoked)
	setExpiry(t, revoked, `idle_expires_at = clock_timestamp() - interval '1 minute'`)
	held := newSession(t, "held")
	setExpiry(t, held, `idle_expires_at = clock_timestamp() - interval '1 minute'`)
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `select 1 from public.sessions where id = $1::uuid for update`, string(held.ID)); err != nil {
		t.Fatal(err)
	}

	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	closed, err := store.CloseExpiredSessions(bounded)
	if err != nil {
		t.Fatalf("CloseExpiredSessions beside a held row = %v", err)
	}
	if closed != 2 {
		t.Errorf("closed %d sessions, want the idle- and absolute-expired ones", closed)
	}
	if ended, idle, _ := endedAt(t, idleExpired); ended == nil || !ended.Equal(idle) {
		t.Errorf("idle-expired session ended at %v, want its idle expiry %s", ended, idle)
	}
	if ended, _, absolute := endedAt(t, absoluteExpired); ended == nil || !ended.Equal(absolute) {
		t.Errorf("absolute-expired session ended at %v, want its absolute expiry %s", ended, absolute)
	}
	if ended, _, _ := endedAt(t, live); ended != nil {
		t.Errorf("live session was closed at %s", ended)
	}
	if ended, _, _ := endedAt(t, revoked); ended == nil || originalRevocation == nil || !ended.Equal(*originalRevocation) {
		t.Errorf("revoked session's revoked_at = %v, want the original %v", ended, originalRevocation)
	}
	if ended, _, _ := endedAt(t, held); ended != nil {
		t.Error("a row held by another transaction was waited for instead of skipped")
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if closed, err := store.CloseExpiredSessions(ctx); err != nil || closed != 1 {
		t.Errorf("second sweep = %d, %v; want the released session closed", closed, err)
	}

	var rows int
	if err := pool.QueryRow(ctx, `select count(*) from public.sessions`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 5 {
		t.Errorf("sessions table holds %d rows, want all 5 kept as history", rows)
	}
	if err := store.ValidateSession(ctx, idleExpired.ID); !errors.Is(err, identity.ErrSessionNotFound) {
		t.Errorf("ValidateSession on a closed session = %v, want ErrSessionNotFound", err)
	}
	if err := store.ExtendSessionIdle(ctx, absoluteExpired.ID, time.Now().Add(time.Hour)); !errors.Is(err, identity.ErrSessionNotFound) {
		t.Errorf("ExtendSessionIdle on a closed session = %v, want ErrSessionNotFound", err)
	}
	var logouts int
	if err := store.RevokeSession(ctx, idleExpired.ID, testEvent(audit.ActionAuthLogout)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from audit_events where action = 'AUTH_LOGOUT'`).Scan(&logouts); err != nil {
		t.Fatal(err)
	}
	if logouts != 1 {
		t.Errorf("logout of a closed session wrote audit events (total %d, want only the earlier one)", logouts)
	}
}
