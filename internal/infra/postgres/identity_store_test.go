package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// unique returns a per-run unique name so tests on the SHARED dbtest.Postgres
// database stay re-runnable against a persistent PORTCULLIS_TEST_DATABASE_URL:
// rows are soft-delete-only (data.md), so a fixed email/name would collide with
// the previous run's unique index entry.
func unique(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// testEvent is a minimal valid audit event for exercising the transactional ops
// in tests whose subject is not the audit trail itself.
func testEvent(action audit.Action) audit.Event {
	return audit.Event{ActorType: audit.ActorUser, Action: action, TargetType: audit.TargetTypeUser, Outcome: audit.OutcomeSucceeded}
}

func TestIdentityStore(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	// Permission catalog and system roles are seeded.
	perms, err := store.ListPermissions(ctx)
	if err != nil {
		t.Fatalf("ListPermissions: %v", err)
	}
	if len(perms) == 0 {
		t.Fatal("permission catalog should be seeded")
	}

	org, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("DefaultOrganizationID: %v", err)
	}
	roleID, err := store.BootstrapRoleID(ctx, org)
	if err != nil {
		t.Fatalf("BootstrapRoleID: %v", err)
	}

	// Create a user with a password and the bootstrap (admin) role.
	email := unique("admin") + "@example.com"
	u, err := store.CreateUser(ctx, email, "Admin")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.SetPassword(ctx, u.ID, "phc-hash"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if err := store.AddMembership(ctx, org, u.ID, roleID); err != nil {
		t.Fatalf("AddMembership: %v", err)
	}

	// The bootstrap (admin) role grants the full catalog.
	userPerms, err := store.PermissionsForUser(ctx, org, u.ID)
	if err != nil {
		t.Fatalf("PermissionsForUser: %v", err)
	}
	if len(userPerms) != len(perms) {
		t.Errorf("admin permissions = %d, want full catalog %d", len(userPerms), len(perms))
	}

	if h, err := store.GetPasswordHash(ctx, u.ID); err != nil || h != "phc-hash" {
		t.Errorf("GetPasswordHash = %q, %v", h, err)
	}
	if got, err := store.GetUserByEmail(ctx, email); err != nil || got.ID != u.ID {
		t.Errorf("GetUserByEmail mismatch: %v", err)
	}
	if _, err := store.GetUserByEmail(ctx, "nobody@example.com"); !errors.Is(err, identity.ErrUserNotFound) {
		t.Errorf("want ErrUserNotFound, got %v", err)
	}

	// Sessions round-trip and revoke. (token_hash is unique — per-run value.)
	now := time.Now()
	sess := identity.NewSession("", u.ID, now, 12*time.Hour, 7*24*time.Hour)
	tokenHash := sha256.Sum256([]byte(unique("raw-token")))
	created, err := store.CreateSession(ctx, sess, tokenHash[:])
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if got, err := store.GetSessionByTokenHash(ctx, tokenHash[:]); err != nil || got.ID != created.ID {
		t.Errorf("GetSessionByTokenHash mismatch: %v", err)
	}
	if err := store.RevokeSession(ctx, created.ID, testEvent(audit.ActionAuthLogout)); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	// OIDC link + session rotation is the only production linking operation;
	// lookup verifies the committed link. ((issuer, subject) is unique per run.)
	subject := unique("sub")
	oidcToken := sha256.Sum256([]byte(unique("oidc-token")))
	oidcSession := identity.NewSession("", u.ID, time.Now(), 12*time.Hour, 7*24*time.Hour)
	if _, err := store.LinkIdentityAndRotateSession(ctx, identity.OIDCIdentity{
		UserID: u.ID, Issuer: "https://accounts.google.com", Subject: subject, Email: email,
	}, oidcSession, oidcToken[:], testEvent(audit.ActionAuthLogin)); err != nil {
		t.Fatalf("LinkIdentityAndRotateSession: %v", err)
	}
	if got, err := store.FindUserBySubject(ctx, "https://accounts.google.com", subject); err != nil || got.ID != u.ID {
		t.Errorf("FindUserBySubject mismatch: %v", err)
	}
	if _, err := store.FindUserBySubject(ctx, "https://accounts.google.com", "nope"); !errors.Is(err, identity.ErrNoLinkedAccount) {
		t.Errorf("want ErrNoLinkedAccount, got %v", err)
	}
}

// Progressive-backoff state (ADR-0006): zero-valued before any failure,
// counted and locked in ONE atomic statement (no lost updates under
// concurrency, no increment/lockout interleave), the lockout never moves
// backward, expired lockouts and stale sub-threshold counters lazily restart
// at 1, and a reset clears the slate without touching clean rows. Everything
// time-related runs on the DATABASE clock inside the statement, which is why
// it is exercised here and not only against the fake.
func TestLoginBackoffStore(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	// The ADR parameters with jitter pinned to the midpoint (factor 1.0).
	params := identity.FailureParams{
		Threshold: 5, Base: time.Minute, Cap: 15 * time.Minute,
		Staleness: 15 * time.Minute, JitterFactor: 1.0,
	}
	// window asserts a lockout expiry sits within tolerance of now+want.
	window := func(t *testing.T, lockedUntil *time.Time, want time.Duration) {
		t.Helper()
		if lockedUntil == nil {
			t.Fatal("expected a lockout")
		}
		got := time.Until(*lockedUntil)
		if got < want-10*time.Second || got > want+10*time.Second {
			t.Errorf("lockout window = %s, want ~%s", got, want)
		}
	}

	email := unique("backoff") + "@example.com"
	u, err := store.CreateUser(ctx, email, "Backoff")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// No row yet: the login query reports a zero-value, unlocked state.
	_, _, b, err := store.GetUserForLogin(ctx, email)
	if err != nil {
		t.Fatalf("GetUserForLogin: %v", err)
	}
	if b.FailureCount != 0 || b.LockedUntil != nil || b.Locked {
		t.Errorf("fresh account backoff = %+v, want zero value", b)
	}

	// Concurrent failures must not lose updates (the upsert is atomic), and
	// crossing the threshold under concurrency imposes a lockout.
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.RecordLoginFailure(ctx, u.ID, params); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("RecordLoginFailure: %v", err)
	}
	_, _, b, err = store.GetUserForLogin(ctx, email)
	if err != nil || b.FailureCount != n {
		t.Fatalf("failure count after %d concurrent failures = %d (%v), want %d", n, b.FailureCount, err, n)
	}
	if !b.Locked {
		t.Fatal("crossing the threshold under concurrency did not lock")
	}

	// Doubling: with the count at 8, the next failure's window is
	// min(1m·2^(9-5), 15m) = 15m (the cap).
	res, err := store.RecordLoginFailure(ctx, u.ID, params)
	if err != nil {
		t.Fatalf("RecordLoginFailure: %v", err)
	}
	if res.FailureCount != n+1 || !res.Locked {
		t.Fatalf("9th failure = %+v, want count 9 and locked", res)
	}
	window(t, res.LockedUntil, 15*time.Minute)

	// greatest(): a shorter concurrent jittered window (factor 0.01 → ~9s from
	// count 10) must not move the existing 15m expiry backward.
	shorter := params
	shorter.JitterFactor = 0.01
	prev := *res.LockedUntil
	res, err = store.RecordLoginFailure(ctx, u.ID, shorter)
	if err != nil {
		t.Fatalf("RecordLoginFailure(shorter): %v", err)
	}
	if res.LockedUntil == nil || res.LockedUntil.Before(prev) {
		t.Errorf("shorter jittered window moved the lockout backward: %v → %v", prev, res.LockedUntil)
	}

	// A reset clears both counter and lockout.
	if err := store.ResetLoginBackoff(ctx, u.ID); err != nil {
		t.Fatalf("ResetLoginBackoff: %v", err)
	}
	if _, _, b, err = store.GetUserForLogin(ctx, email); err != nil || b.FailureCount != 0 || b.LockedUntil != nil || b.Locked {
		t.Fatalf("backoff after reset = %+v (%v), want cleared", b, err)
	}

	// An expired lockout lazily restarts the counter at 1 on the next failure
	// ("resets on expiry"): seed failures to a lockout, expire it in the DB,
	// then fail once more.
	for i := 0; i < 5; i++ {
		if _, err := store.RecordLoginFailure(ctx, u.ID, params); err != nil {
			t.Fatalf("RecordLoginFailure: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `update login_backoff set locked_until = now() - interval '1 second' where user_id = $1::uuid`, string(u.ID)); err != nil {
		t.Fatalf("expire lockout: %v", err)
	}
	res, err = store.RecordLoginFailure(ctx, u.ID, params)
	if err != nil {
		t.Fatalf("RecordLoginFailure after expiry: %v", err)
	}
	if res.FailureCount != 1 || res.Locked || res.LockedUntil != nil {
		t.Errorf("state after expired lockout = %+v, want count 1, unlocked (lazy reset)", res)
	}

	// A stale sub-threshold counter (last failure older than the staleness
	// window, no lockout) also restarts at 1.
	if _, err := store.RecordLoginFailure(ctx, u.ID, params); err != nil {
		t.Fatalf("RecordLoginFailure: %v", err)
	}
	if _, err := pool.Exec(ctx, `update login_backoff set last_failure_at = now() - interval '20 minutes' where user_id = $1::uuid`, string(u.ID)); err != nil {
		t.Fatalf("age last_failure_at: %v", err)
	}
	res, err = store.RecordLoginFailure(ctx, u.ID, params)
	if err != nil {
		t.Fatalf("RecordLoginFailure after staleness: %v", err)
	}
	if res.FailureCount != 1 || res.Locked {
		t.Errorf("state after stale counter = %+v, want count 1, unlocked", res)
	}
}

// The first lockout windows are exact under a pinned jitter factor: 1m at the
// threshold, 2m on the next failure (ADR-0006 doubling), evaluated end-to-end
// through the SQL expression.
func TestLoginBackoffWindows(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)
	params := identity.FailureParams{
		Threshold: 5, Base: time.Minute, Cap: 15 * time.Minute,
		Staleness: 15 * time.Minute, JitterFactor: 1.0,
	}

	u, err := store.CreateUser(ctx, unique("windows")+"@example.com", "W")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	var res identity.LoginBackoff
	for i := 0; i < 5; i++ {
		if res, err = store.RecordLoginFailure(ctx, u.ID, params); err != nil {
			t.Fatalf("RecordLoginFailure %d: %v", i+1, err)
		}
	}
	if res.FailureCount != 5 || !res.Locked || res.LockedUntil == nil {
		t.Fatalf("5th failure = %+v, want locked", res)
	}
	if got := time.Until(*res.LockedUntil); got < 50*time.Second || got > 70*time.Second {
		t.Errorf("first window = %s, want ~1m", got)
	}
	if res, err = store.RecordLoginFailure(ctx, u.ID, params); err != nil {
		t.Fatalf("6th RecordLoginFailure: %v", err)
	}
	if got := time.Until(*res.LockedUntil); got < 110*time.Second || got > 130*time.Second {
		t.Errorf("second window = %s, want ~2m (doubled)", got)
	}
}

// A duplicate email (case-insensitive, per the lower(email) unique index) maps to
// the domain sentinel ErrEmailTaken, not a raw driver error, so callers can tell a
// conflict from an infrastructure failure across the port boundary.
func TestCreateUserDuplicateEmailMapsToErrEmailTaken(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	email := unique("dup") + "@example.com"
	if _, err := store.CreateUser(ctx, email, "First"); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	// Same address in different case must still collide on the lower(email) index.
	_, err := store.CreateUser(ctx, "DUP"+email[3:], "Second")
	if !errors.Is(err, identity.ErrEmailTaken) {
		t.Errorf("duplicate CreateUser = %v, want ErrEmailTaken", err)
	}
}

// A second revocation of the same session (concurrent double logout) must not
// overwrite the original revoked_at — forensic evidence of WHEN the session
// actually died — nor append a second AUTH_LOGOUT event: the audit trail
// mirrors real state changes only (ADR-0009).
func TestRevokeSessionIsIdempotent(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	u, err := store.CreateUser(ctx, unique("revoke")+"@example.com", "R")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sess := identity.NewSession("", u.ID, time.Now(), 12*time.Hour, 7*24*time.Hour)
	tokenHash := sha256.Sum256([]byte(unique("revoke-token")))
	created, err := store.CreateSession(ctx, sess, tokenHash[:])
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	evt := testEvent(audit.ActionAuthLogout)
	evt.TargetID = string(u.ID) // so this test's events are countable on a shared DB
	if err := store.RevokeSession(ctx, created.ID, evt); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	var firstRevokedAt time.Time
	if err := pool.QueryRow(ctx, `select revoked_at from sessions where id = $1::uuid`, string(created.ID)).Scan(&firstRevokedAt); err != nil {
		t.Fatalf("read revoked_at: %v", err)
	}

	// Second revoke: a no-op, not an error — and no new evidence.
	if err := store.RevokeSession(ctx, created.ID, evt); err != nil {
		t.Fatalf("second RevokeSession should be a no-op, got: %v", err)
	}
	var again time.Time
	if err := pool.QueryRow(ctx, `select revoked_at from sessions where id = $1::uuid`, string(created.ID)).Scan(&again); err != nil {
		t.Fatalf("re-read revoked_at: %v", err)
	}
	if !again.Equal(firstRevokedAt) {
		t.Errorf("revoked_at overwritten: first %v, now %v", firstRevokedAt, again)
	}
	var events int
	if err := pool.QueryRow(ctx,
		`select count(*) from audit_events where action = 'AUTH_LOGOUT' and target_id = $1`, string(u.ID),
	).Scan(&events); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if events != 1 {
		t.Errorf("AUTH_LOGOUT events = %d, want exactly 1 (no trail without a state change)", events)
	}
}

// Authentication and the post-CSRF idle slide are separate operations. If the
// idle deadline passes between them, the UPDATE must not resurrect the session.
func TestExtendSessionIdleDoesNotResurrectExpiredSession(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	u, err := store.CreateUser(ctx, unique("expired-session")+"@example.com", "Expired")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sess := identity.NewSession("", u.ID, time.Now(), time.Hour, 24*time.Hour)
	hash := sha256.Sum256([]byte(unique("expired-session-token")))
	created, err := store.CreateSession(ctx, sess, hash[:])
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`update public.sessions set idle_expires_at = now() - interval '1 second' where id = $1::uuid`,
		string(created.ID)); err != nil {
		t.Fatalf("expire session: %v", err)
	}

	if err := store.ExtendSessionIdle(ctx, created.ID, time.Now().Add(time.Hour)); !errors.Is(err, identity.ErrSessionNotFound) {
		t.Fatalf("ExtendSessionIdle = %v, want ErrSessionNotFound", err)
	}
	var resurrected bool
	if err := pool.QueryRow(ctx,
		`select idle_expires_at > now() from public.sessions where id = $1::uuid`,
		string(created.ID)).Scan(&resurrected); err != nil {
		t.Fatalf("read session expiry: %v", err)
	}
	if resurrected {
		t.Fatal("expired session was resurrected by idle slide")
	}
}

// The idle-slide UPDATE carries two normative guards (ADR-0006): greatest()
// so a late-arriving older request can never move the expiry backward, and
// least() so the idle expiry never exceeds the absolute one.
func TestExtendSessionIdleGuardsBackwardAndAbsolute(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	u, err := store.CreateUser(ctx, unique("idle-guards")+"@example.com", "Idle")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sess := identity.NewSession("", u.ID, time.Now(), time.Hour, 24*time.Hour)
	hash := sha256.Sum256([]byte(unique("idle-guards-token")))
	created, err := store.CreateSession(ctx, sess, hash[:])
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	var baseline time.Time
	if err := pool.QueryRow(ctx,
		`select idle_expires_at from public.sessions where id = $1::uuid`,
		string(created.ID)).Scan(&baseline); err != nil {
		t.Fatalf("read baseline: %v", err)
	}

	// A stale request carrying an EARLIER deadline must not regress the expiry.
	if err := store.ExtendSessionIdle(ctx, created.ID, time.Now().Add(30*time.Minute)); err != nil {
		t.Fatalf("ExtendSessionIdle (earlier): %v", err)
	}
	var unchanged bool
	if err := pool.QueryRow(ctx,
		`select idle_expires_at = $2 from public.sessions where id = $1::uuid`,
		string(created.ID), baseline).Scan(&unchanged); err != nil {
		t.Fatalf("read after earlier slide: %v", err)
	}
	if !unchanged {
		t.Error("an earlier deadline moved idle_expires_at backward; greatest() guard is broken")
	}

	// A deadline past the absolute expiry must clamp to it.
	if err := store.ExtendSessionIdle(ctx, created.ID, time.Now().Add(1000*time.Hour)); err != nil {
		t.Fatalf("ExtendSessionIdle (huge): %v", err)
	}
	var capped bool
	if err := pool.QueryRow(ctx,
		`select idle_expires_at = absolute_expires_at from public.sessions where id = $1::uuid`,
		string(created.ID)).Scan(&capped); err != nil {
		t.Fatalf("read after huge slide: %v", err)
	}
	if !capped {
		t.Error("idle_expires_at exceeded absolute_expires_at; least() clamp is broken")
	}
}

// Concurrent first-run bootstraps must serialize (advisory lock) so exactly one
// admin is created — the rest see ErrAlreadyBootstrapped, never a duplicate or a
// half-created user. Runs on an isolated database since it asserts on the whole
// users table.
func TestBootstrapAdminSerializesConcurrent(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = store.BootstrapAdmin(ctx, "admin@example.com", "Admin", "phc-hash", testEvent(audit.ActionAuthBootstrap))
		}(i)
	}
	wg.Wait()

	var ok, already int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, identity.ErrAlreadyBootstrapped):
			already++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 || already != n-1 {
		t.Errorf("want 1 success + %d already-bootstrapped, got %d + %d", n-1, ok, already)
	}
	if c, err := store.CountUsers(ctx); err != nil || c != 1 {
		t.Errorf("want exactly 1 user, got %d (%v)", c, err)
	}
}

// Concurrent logins (RotateSession) must serialize so exactly one session stays
// active — the per-user advisory lock makes revoke+insert atomic.
func TestRotateSessionLeavesOneActive(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	u, err := store.BootstrapAdmin(ctx, "admin@example.com", "Admin", "phc-hash", testEvent(audit.ActionAuthBootstrap))
	if err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			now := time.Now()
			sess := identity.NewSession("", u.ID, now, 12*time.Hour, 7*24*time.Hour)
			h := sha256.Sum256([]byte{byte(i)})
			if _, err := store.RotateSession(ctx, u.ID, sess, h[:], testEvent(audit.ActionAuthLogin)); err != nil {
				t.Errorf("RotateSession: %v", err)
			}
		}(i)
	}
	wg.Wait()

	var active int
	if err := pool.QueryRow(ctx,
		`select count(*) from sessions where user_id = $1::uuid and revoked_at is null`,
		string(u.ID)).Scan(&active); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if active != 1 {
		t.Errorf("active sessions = %d, want exactly 1", active)
	}
}

// A soft-deleted role must stop granting its permissions even though the
// membership still references it (FKs are RESTRICT, so the row lingers).
func TestPermissionsExcludeSoftDeletedRole(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	org, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("DefaultOrganizationID: %v", err)
	}

	// A throwaway custom role granting exactly one permission.
	var roleID string
	if err := pool.QueryRow(ctx,
		`insert into roles (organization_id, name) values ($1::uuid, $2) returning id::text`,
		string(org), unique("tmp-role")).Scan(&roleID); err != nil {
		t.Fatalf("insert role: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`insert into role_permissions (role_id, permission_key) values ($1::uuid, 'users.list')`,
		roleID); err != nil {
		t.Fatalf("grant permission: %v", err)
	}

	u, err := store.CreateUser(ctx, unique("perm")+"@example.com", "Perm")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.AddMembership(ctx, org, u.ID, identity.RoleID(roleID)); err != nil {
		t.Fatalf("AddMembership: %v", err)
	}

	if perms, err := store.PermissionsForUser(ctx, org, u.ID); err != nil || len(perms) != 1 {
		t.Fatalf("active role should grant 1 permission, got %d (%v)", len(perms), err)
	}

	// Soft-delete the role; the membership row stays but grants nothing.
	if _, err := pool.Exec(ctx, `update roles set deleted_at = now() where id = $1::uuid`, roleID); err != nil {
		t.Fatalf("soft-delete role: %v", err)
	}
	if after, err := store.PermissionsForUser(ctx, org, u.ID); err != nil || len(after) != 0 {
		t.Errorf("soft-deleted role should grant nothing, got %d (%v)", len(after), err)
	}
}

// insertRole creates a role in org granting exactly one permission, returning its id.
func insertRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool, org, name, perm string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`insert into roles (organization_id, name) values ($1::uuid, $2) returning id::text`,
		org, name).Scan(&id); err != nil {
		t.Fatalf("insert role %q: %v", name, err)
	}
	if _, err := pool.Exec(ctx,
		`insert into role_permissions (role_id, permission_key) values ($1::uuid, $2)`, id, perm); err != nil {
		t.Fatalf("grant %q: %v", perm, err)
	}
	return id
}

// Permission resolution is per-organization: a user with different roles in two
// orgs gets only the queried org's permissions, never the cross-org union (ADR-0004).
func TestPermissionsAreOrgScoped(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	orgA, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("DefaultOrganizationID: %v", err)
	}
	var orgB string
	if err := pool.QueryRow(ctx,
		`insert into organizations (slug, name) values ('org-b', 'Org B') returning id::text`).Scan(&orgB); err != nil {
		t.Fatalf("insert org: %v", err)
	}

	roleA := insertRole(t, ctx, pool, string(orgA), "scoped-a", "users.list")
	roleB := insertRole(t, ctx, pool, orgB, "scoped-b", "roles.delete")

	u, err := store.CreateUser(ctx, "multi@example.com", "Multi")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.AddMembership(ctx, orgA, u.ID, identity.RoleID(roleA)); err != nil {
		t.Fatalf("AddMembership A: %v", err)
	}
	if err := store.AddMembership(ctx, identity.OrganizationID(orgB), u.ID, identity.RoleID(roleB)); err != nil {
		t.Fatalf("AddMembership B: %v", err)
	}

	if a, err := store.PermissionsForUser(ctx, orgA, u.ID); err != nil || len(a) != 1 || a[0] != "users.list" {
		t.Errorf("org A perms = %v (%v), want [users.list]", a, err)
	}
	if b, err := store.PermissionsForUser(ctx, identity.OrganizationID(orgB), u.ID); err != nil || len(b) != 1 || b[0] != "roles.delete" {
		t.Errorf("org B perms = %v (%v), want [roles.delete]", b, err)
	}
}
