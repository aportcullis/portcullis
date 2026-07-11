package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Progressive-backoff scenarios (ADR-0006 Parameters): after 5 consecutive
// password failures the account locks for 1 min, doubling per further failure
// to a 15 min cap, ±20% jitter on the expiry; the counter resets on success,
// expiry, or staleness, and a locked account is indistinguishable from a
// wrong password.

// newBackoffFixture builds a service whose clock and jitter are pinned and
// SHARED with the fake repo (the real store evaluates expiry on the database
// clock, so both must advance together). Move time via *clock. A nil hasher
// gets the plain fake.
func newBackoffFixture(t *testing.T, jitter float64, hasher auth.PasswordHasher) (*fakeRepo, *auth.Service, *capturingRecorder, *time.Time) {
	t.Helper()
	if hasher == nil {
		hasher = fakeHasher{}
	}
	now := time.Now()
	clock := &now
	repo := newFake()
	repo.clock = func() time.Time { return *clock }
	rec := &capturingRecorder{}
	svc, err := auth.New(repo, hasher, fakeCSRF{}, rec, auth.Config{})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	svc.WithClock(func() time.Time { return *clock }).WithJitter(func() float64 { return jitter })
	return repo, svc, rec, clock
}

// failLogins performs n wrong-password attempts, each of which must get the
// generic credential rejection.
func failLogins(t *testing.T, svc *auth.Service, email string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := svc.Login(context.Background(), email, "wrong-password-xx"); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatalf("wrong-password attempt %d = %v, want ErrInvalidCredentials", i+1, err)
		}
	}
}

// recordingHasher captures the encoded hash each Verify ran against, so a test
// can pin that a locked attempt verified the timing-equalizer dummy and NEVER
// the real stored hash.
type recordingHasher struct {
	fakeHasher
	encoded []string
}

func (h *recordingHasher) Verify(ctx context.Context, password, encoded string) (bool, bool, error) {
	h.encoded = append(h.encoded, encoded)
	return h.fakeHasher.Verify(ctx, password, encoded)
}

// After the 5th consecutive failure the account is locked: even the CORRECT
// password gets the same generic rejection, no session is issued, and the real
// hash is never verified (so the lockout leaks nothing through timing either).
func TestLoginLocksAfterFiveConsecutiveFailures(t *testing.T) {
	t.Parallel()
	hasher := &recordingHasher{}
	repo, svc, _, _ := newBackoffFixture(t, 0.5, hasher)
	u := bootstrapUser(t, svc)

	failLogins(t, svc, u.Email, 5)

	before := len(hasher.encoded)
	sess, err := svc.Login(context.Background(), u.Email, "hunter2-secretz")
	if !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("correct password while locked = %v, want ErrInvalidCredentials", err)
	}
	if sess.Token != "" {
		t.Error("locked login must not issue a session")
	}
	// Exactly one (timing-equalizing) verify ran, and not against the real hash.
	stored := repo.passwords[u.ID]
	verified := hasher.encoded[before:]
	if len(verified) != 1 {
		t.Fatalf("locked attempt ran %d verifies, want 1 (the dummy)", len(verified))
	}
	if verified[0] == stored {
		t.Error("locked attempt verified the REAL stored hash; it must use the dummy so a correct password is unobservable")
	}
	// The locked attempt still counts (doubling pressure while hammered).
	if got := repo.backoff[u.ID].FailureCount; got != 6 {
		t.Errorf("failure count after locked attempt = %d, want 6", got)
	}
}

// The first lockout window is 1 minute, spread ±20% by the injected jitter:
// factor = 0.8 + 0.4·j.
func TestLockoutWindowStartsAtOneMinuteWithJitter(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		jitter float64
		want   time.Duration
	}{
		{"low edge", 0, 48 * time.Second},
		{"midpoint", 0.5, 60 * time.Second},
		{"high edge", 1, 72 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo, svc, _, clock := newBackoffFixture(t, tc.jitter, nil)
			u := bootstrapUser(t, svc)

			failLogins(t, svc, u.Email, 5)

			b := repo.backoff[u.ID]
			if b.LockedUntil == nil {
				t.Fatal("5th failure did not impose a lockout")
			}
			if want := clock.Add(tc.want); !b.LockedUntil.Equal(want) {
				t.Errorf("LockedUntil = now+%s, want now+%s", b.LockedUntil.Sub(*clock), tc.want)
			}
		})
	}
}

// Failures during a lockout keep doubling the window — 2m, 4m, 8m, then capped
// at 15m — and the expiry never moves backward.
func TestLockoutDoublesPerFailureDuringLockoutUpToCap(t *testing.T) {
	t.Parallel()
	repo, svc, _, clock := newBackoffFixture(t, 0.5, nil) // factor 1.0: exact windows
	u := bootstrapUser(t, svc)

	failLogins(t, svc, u.Email, 5)

	wants := []time.Duration{
		2 * time.Minute,  // failure 6
		4 * time.Minute,  // failure 7
		8 * time.Minute,  // failure 8
		15 * time.Minute, // failure 9: 16m capped at 15m
		15 * time.Minute, // failure 10: stays at the cap
	}
	prev := *repo.backoff[u.ID].LockedUntil
	for i, want := range wants {
		failLogins(t, svc, u.Email, 1)
		b := repo.backoff[u.ID]
		if b.LockedUntil == nil {
			t.Fatalf("failure %d cleared the lockout", 6+i)
		}
		if wantAt := clock.Add(want); !b.LockedUntil.Equal(wantAt) {
			t.Errorf("failure %d: LockedUntil = now+%s, want now+%s", 6+i, b.LockedUntil.Sub(*clock), want)
		}
		if b.LockedUntil.Before(prev) {
			t.Errorf("failure %d moved LockedUntil backward (%v → %v)", 6+i, prev, *b.LockedUntil)
		}
		prev = *b.LockedUntil
	}
}

// Once the lockout expires the slate is clean: the next failure restarts the
// counter at 1 (no immediate re-lock), and the correct password signs in.
func TestLockoutExpiryResetsCounter(t *testing.T) {
	t.Parallel()
	repo, svc, _, clock := newBackoffFixture(t, 0.5, nil)
	u := bootstrapUser(t, svc)

	failLogins(t, svc, u.Email, 5)
	if repo.backoff[u.ID].LockedUntil == nil {
		t.Fatal("no lockout after 5 failures")
	}

	*clock = clock.Add(2 * time.Minute) // past the 1m window

	failLogins(t, svc, u.Email, 1)
	b := repo.backoff[u.ID]
	if b.FailureCount != 1 {
		t.Errorf("failure count after expiry = %d, want 1 (restarted)", b.FailureCount)
	}
	if b.LockedUntil != nil {
		t.Errorf("a single post-expiry failure re-locked until %v", *b.LockedUntil)
	}
	if _, err := svc.Login(context.Background(), u.Email, "hunter2-secretz"); err != nil {
		t.Errorf("correct password after expiry = %v, want success", err)
	}
}

// A sub-threshold counter goes stale after the staleness window (= the cap):
// months-old typos must not count toward a fresh lockout (ADR-0006 amendment).
func TestStaleSubThresholdCounterResets(t *testing.T) {
	t.Parallel()
	repo, svc, _, clock := newBackoffFixture(t, 0.5, nil)
	u := bootstrapUser(t, svc)

	failLogins(t, svc, u.Email, 4) // one short of the threshold, no lockout

	*clock = clock.Add(16 * time.Minute) // past the 15m staleness window

	failLogins(t, svc, u.Email, 1)
	b := repo.backoff[u.ID]
	if b.FailureCount != 1 {
		t.Errorf("failure count after staleness = %d, want 1 (restarted)", b.FailureCount)
	}
	if b.LockedUntil != nil {
		t.Errorf("stale counter still locked the account until %v", *b.LockedUntil)
	}
}

// A successful login resets the counter: failures before and after it never
// add up to a lockout.
func TestSuccessResetsCounter(t *testing.T) {
	t.Parallel()
	repo, svc, _, _ := newBackoffFixture(t, 0.5, nil)
	u := bootstrapUser(t, svc)

	failLogins(t, svc, u.Email, 4)
	if _, err := svc.Login(context.Background(), u.Email, "hunter2-secretz"); err != nil {
		t.Fatalf("login at 4 failures = %v, want success", err)
	}
	if repo.resetWrites != 1 {
		t.Errorf("reset writes = %d, want 1", repo.resetWrites)
	}

	failLogins(t, svc, u.Email, 4)
	if repo.backoff[u.ID].LockedUntil != nil {
		t.Fatal("4 post-success failures locked the account; the counter did not reset")
	}
	failLogins(t, svc, u.Email, 1)
	if repo.backoff[u.ID].LockedUntil == nil {
		t.Error("5th consecutive failure should lock")
	}
}

// A clean successful login (no prior failures) must not pay a backoff row
// write — the reset statement's WHERE leaves clean rows unwritten.
func TestSuccessfulLoginSkipsBackoffWriteWhenClean(t *testing.T) {
	t.Parallel()
	repo, svc, _, _ := newBackoffFixture(t, 0.5, nil)
	u := bootstrapUser(t, svc)

	if _, err := svc.Login(context.Background(), u.Email, "hunter2-secretz"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if repo.resetWrites != 0 {
		t.Errorf("clean login performed %d reset row writes, want 0", repo.resetWrites)
	}
}

// A locked account and a wrong password are indistinguishable: the same
// sentinel and exactly one hasher.Verify per attempt (no extra or missing
// hashing work to time).
func TestLockedResponseIndistinguishableFromWrongPassword(t *testing.T) {
	t.Parallel()
	hasher := &countingHasher{}
	_, svc, _, _ := newBackoffFixture(t, 0.5, hasher)
	u := bootstrapUser(t, svc)

	hasher.verifies.Store(0)
	_, wrongErr := svc.Login(context.Background(), u.Email, "wrong-password-xx")
	unlockedVerifies := hasher.verifies.Load()

	failLogins(t, svc, u.Email, 4) // now at 5 failures: locked

	hasher.verifies.Store(0)
	_, lockedErr := svc.Login(context.Background(), u.Email, "wrong-password-xx")
	lockedVerifies := hasher.verifies.Load()

	if !errors.Is(wrongErr, identity.ErrInvalidCredentials) || !errors.Is(lockedErr, identity.ErrInvalidCredentials) {
		t.Errorf("errors differ: unlocked %v, locked %v — both must be ErrInvalidCredentials", wrongErr, lockedErr)
	}
	if unlockedVerifies != 1 || lockedVerifies != 1 {
		t.Errorf("verify calls: unlocked %d, locked %d — both must be exactly 1", unlockedVerifies, lockedVerifies)
	}
}

// Unknown emails have no account and therefore no counter — attempted emails
// are never persisted (ADR-0006: the backoff is per-account).
func TestUnknownEmailDoesNotTouchCounter(t *testing.T) {
	t.Parallel()
	repo, svc, _, _ := newBackoffFixture(t, 0.5, nil)
	bootstrapUser(t, svc)

	for i := 0; i < 6; i++ {
		if _, err := svc.Login(context.Background(), "ghost@example.com", "wrong-password-xx"); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatalf("unknown-email attempt = %v", err)
		}
	}
	if repo.failureWrites != 0 {
		t.Errorf("unknown email wrote the counter %d times, want 0", repo.failureWrites)
	}
}

// Password probes against an OIDC-only account (no password credential) are
// failed password attempts of a known account: they count and eventually lock.
func TestOIDCOnlyAccountFailuresCount(t *testing.T) {
	t.Parallel()
	repo, svc, _, _ := newBackoffFixture(t, 0.5, nil)
	u, err := repo.CreateUser(context.Background(), "sso-only@example.com", "SSO")
	if err != nil {
		t.Fatal(err)
	}

	failLogins(t, svc, u.Email, 5)
	b := repo.backoff[u.ID]
	if b.FailureCount != 5 {
		t.Errorf("failure count = %d, want 5", b.FailureCount)
	}
	if b.LockedUntil == nil {
		t.Error("5 password probes of an OIDC-only account should lock it")
	}
}

// The backoff writes are best-effort: a counter-store outage degrades the
// backoff, never the login response — and a failure that could not be counted
// must not be audit-tagged as a lockout (the trail records only real state).
func TestBackoffWriteFailureIsBestEffort(t *testing.T) {
	t.Parallel()
	repo, svc, rec, _ := newBackoffFixture(t, 0.5, nil)
	u := bootstrapUser(t, svc)

	repo.failBackoffWrites = true
	if _, err := svc.Login(context.Background(), u.Email, "wrong-password-xx"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("wrong password with failing counter = %v, want ErrInvalidCredentials", err)
	}
	for _, e := range rec.all() {
		if e.Metadata != nil {
			t.Errorf("failure event with a failed counter write carries metadata %v, want none", e.Metadata)
		}
	}
	// Seed prior failures so the success path attempts (and fails) the reset.
	repo.backoff[u.ID] = identity.LoginBackoff{FailureCount: 2}
	if _, err := svc.Login(context.Background(), u.Email, "hunter2-secretz"); err != nil {
		t.Errorf("correct login with failing reset = %v, want success", err)
	}
}

// ctxAwareBackoffRepo propagates request-context cancellation into the counter
// write, the way the real pgx-backed store would.
type ctxAwareBackoffRepo struct{ *fakeRepo }

func (r ctxAwareBackoffRepo) RecordLoginFailure(ctx context.Context, id identity.UserID, p identity.FailureParams) (identity.LoginBackoff, error) {
	if err := ctx.Err(); err != nil {
		return identity.LoginBackoff{}, err
	}
	return r.fakeRepo.RecordLoginFailure(ctx, id, p)
}

// The counter write detaches from the request context: an attacker must not be
// able to skip the counter by disconnecting the moment the verify completes.
func TestFailureCountSurvivesCancelledContext(t *testing.T) {
	t.Parallel()
	repo := newFake()
	svc, err := auth.New(ctxAwareBackoffRepo{repo}, fakeHasher{}, fakeCSRF{}, &capturingRecorder{}, auth.Config{})
	if err != nil {
		t.Fatal(err)
	}
	u := bootstrapUser(t, svc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// The plain fake ignores ctx on the lookup and the verify, so the flow reaches
	// the counter write with a dead request context.
	if _, err := svc.Login(ctx, u.Email, "wrong-password-xx"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("Login = %v, want ErrInvalidCredentials", err)
	}
	if got := repo.backoff[u.ID].FailureCount; got != 1 {
		t.Errorf("failure count = %d, want 1 (the write must detach from cancellation)", got)
	}
}

// Once the threshold is crossed the failure audit event is tagged from the
// RETURNED state, so lockouts stay queryable without a client-visible signal
// (ADR-0009 metadata) and the trail never claims a lockout that doesn't exist.
func TestLockoutTaggedInFailureAudit(t *testing.T) {
	t.Parallel()
	_, svc, rec, _ := newBackoffFixture(t, 0.5, nil)
	u := bootstrapUser(t, svc)

	failLogins(t, svc, u.Email, 6)

	events := rec.all()
	if len(events) != 6 {
		t.Fatalf("recorded %d failure events, want 6", len(events))
	}
	for i, e := range events[:4] {
		if e.Metadata != nil {
			t.Errorf("pre-threshold failure %d carries metadata %v, want none", i+1, e.Metadata)
		}
	}
	for i, e := range events[4:] {
		n := 5 + i
		if e.Metadata == nil {
			t.Fatalf("failure %d (locked) carries no metadata", n)
		}
		if locked, _ := e.Metadata["lockout"].(bool); !locked {
			t.Errorf("failure %d metadata lockout = %v, want true", n, e.Metadata["lockout"])
		}
		if got, _ := e.Metadata["backoff_failures"].(int); got != n {
			t.Errorf("failure %d metadata backoff_failures = %v, want %d", n, e.Metadata["backoff_failures"], n)
		}
		if e.Action != audit.ActionAuthLogin || e.Outcome != audit.OutcomeFailed {
			t.Errorf("failure %d = %s/%s, want AUTH_LOGIN/FAILED (no new audit action)", n, e.Action, e.Outcome)
		}
	}
}

// Zero config takes the ADR defaults; a nonsensical config refuses construction.
func TestNewValidatesBackoffConfig(t *testing.T) {
	t.Parallel()
	if _, err := auth.New(newFake(), fakeHasher{}, fakeCSRF{}, &capturingRecorder{}, auth.Config{}); err != nil {
		t.Errorf("zero backoff config = %v, want defaults to apply", err)
	}
	bad := []auth.Config{
		{BackoffThreshold: -1},
		{BackoffBase: -time.Minute},
		{BackoffBase: 10 * time.Minute, BackoffCap: time.Minute},
	}
	for i, cfg := range bad {
		if svc, err := auth.New(newFake(), fakeHasher{}, fakeCSRF{}, &capturingRecorder{}, cfg); err == nil || svc != nil {
			t.Errorf("bad config %d = (%v, %v), want nil service + error", i, svc, err)
		}
	}
}

// Guard: the backoff caps must not interfere with an unrelated oversized-input
// rejection (which exits before the user is even resolved).
func TestOversizedInputStillSkipsCounter(t *testing.T) {
	t.Parallel()
	repo, svc, _, _ := newBackoffFixture(t, 0.5, nil)
	u := bootstrapUser(t, svc)

	if _, err := svc.Login(context.Background(), u.Email, strings.Repeat("x", 2000)); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("oversized password = %v", err)
	}
	if repo.failureWrites != 0 {
		t.Errorf("oversized input wrote the counter %d times, want 0 (rejected before lookup)", repo.failureWrites)
	}
}
