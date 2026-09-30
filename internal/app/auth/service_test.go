package auth_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

type fakeRepo struct {
	users     map[identity.UserID]identity.User
	emails    map[string]identity.UserID
	passwords map[identity.UserID]string
	sessions  map[identity.SessionID]identity.Session
	byHash    map[string]identity.SessionID
	oidc      map[string]identity.UserID
	backoff   map[identity.UserID]identity.LoginBackoff
	seq       int
	// clock is the repo's now() — the real store's lazy expiry-reset runs on the database clock, so tests that advance the service clock set this too.
	clock func() time.Time

	txEvents []audit.Event

	lastFailure map[identity.UserID]time.Time

	extendCalls      int
	setPasswordCalls int
	failureWrites    int
	resetWrites      int
	// failBackoffWrites makes the backoff writes fail, to prove they are best-effort (a counter outage must never change the login response).
	failBackoffWrites bool

	failOIDCComplete bool
}

func newFake() *fakeRepo {
	return &fakeRepo{
		users:       map[identity.UserID]identity.User{},
		emails:      map[string]identity.UserID{},
		passwords:   map[identity.UserID]string{},
		sessions:    map[identity.SessionID]identity.Session{},
		byHash:      map[string]identity.SessionID{},
		oidc:        map[string]identity.UserID{},
		backoff:     map[identity.UserID]identity.LoginBackoff{},
		lastFailure: map[identity.UserID]time.Time{},
		clock:       time.Now,
	}
}

func (f *fakeRepo) next(prefix string) string {
	f.seq++
	return prefix + string(rune('0'+f.seq))
}

func (f *fakeRepo) CountUsers(context.Context) (int64, error) { return int64(len(f.users)), nil }
func (f *fakeRepo) BootstrapAdmin(ctx context.Context, email, displayName, passwordHash string, evt audit.Event) (identity.User, error) {
	if len(f.users) > 0 {
		return identity.User{}, identity.ErrAlreadyBootstrapped
	}
	u, err := f.CreateUser(ctx, email, displayName)
	if err != nil {
		return identity.User{}, err
	}
	f.passwords[u.ID] = passwordHash
	evt.OrganizationID = "org-default"
	evt.ActorUserID = &u.ID
	evt.TargetType = audit.TargetTypeUser
	evt.TargetID = string(u.ID)
	f.txEvents = append(f.txEvents, evt)
	return u, nil
}
func (f *fakeRepo) DefaultOrganizationID(context.Context) (identity.OrganizationID, error) {
	return "org-default", nil
}
func (f *fakeRepo) CreateUser(_ context.Context, email, displayName string) (identity.User, error) {
	u := identity.User{ID: identity.UserID(f.next("user-")), Email: email, DisplayName: displayName, Status: identity.StatusActive, CreatedAt: time.Now()}
	f.users[u.ID] = u
	f.emails[email] = u.ID
	return u, nil
}
func (f *fakeRepo) GetUserByEmail(_ context.Context, email string) (identity.User, error) {
	id, ok := f.emails[email]
	if !ok {
		return identity.User{}, identity.ErrUserNotFound
	}
	return f.users[id], nil
}
func (f *fakeRepo) GetUserForLogin(_ context.Context, email string) (identity.User, string, identity.LoginBackoff, error) {
	id, ok := f.emails[email]
	if !ok {
		return identity.User{}, "", identity.LoginBackoff{}, identity.ErrUserNotFound
	}
	b := f.backoff[id]
	// Like the real query, the locked flag is evaluated at read time on the repo's clock (the stand-in for the database clock).
	b.Locked = b.LockedUntil != nil && b.LockedUntil.After(f.clock())
	return f.users[id], f.passwords[id], b, nil
}

func (f *fakeRepo) RecordLoginFailure(_ context.Context, id identity.UserID, p identity.FailureParams) (identity.LoginBackoff, error) {
	f.failureWrites++
	if f.failBackoffWrites {
		return identity.LoginBackoff{}, errors.New("backoff store down")
	}
	now := f.clock()
	b := f.backoff[id]

	expired := b.LockedUntil != nil && !b.LockedUntil.After(now)
	stale := b.LockedUntil == nil && b.FailureCount > 0 && !f.lastFailure[id].After(now.Add(-p.Staleness))
	if expired || stale {
		b = identity.LoginBackoff{FailureCount: 1}
	} else {
		b.FailureCount++
	}
	if b.FailureCount >= p.Threshold {
		exp := min(max(b.FailureCount-p.Threshold, 0), 30)
		w := p.Base * (1 << exp)
		if w > p.Cap || w <= 0 {
			w = p.Cap
		}
		until := now.Add(time.Duration(float64(w) * p.JitterFactor))
		// greatest(): a shorter concurrent jittered window must not shrink it.
		if b.LockedUntil == nil || until.After(*b.LockedUntil) {
			b.LockedUntil = &until
		}
	}
	b.Locked = b.LockedUntil != nil && b.LockedUntil.After(now)
	f.backoff[id] = b
	f.lastFailure[id] = now
	return b, nil
}

func (f *fakeRepo) ResetLoginBackoff(_ context.Context, id identity.UserID) error {
	if f.failBackoffWrites {
		return errors.New("backoff store down")
	}

	if b, ok := f.backoff[id]; ok && (b.FailureCount > 0 || b.LockedUntil != nil) {
		f.resetWrites++
		delete(f.backoff, id)
	}
	return nil
}
func (f *fakeRepo) GetUserByID(_ context.Context, id identity.UserID) (identity.User, error) {
	u, ok := f.users[id]
	if !ok {
		return identity.User{}, identity.ErrUserNotFound
	}
	return u, nil
}
func (f *fakeRepo) SetPassword(_ context.Context, id identity.UserID, phc string) error {
	f.setPasswordCalls++
	f.passwords[id] = phc
	return nil
}
func (f *fakeRepo) GetPasswordHash(_ context.Context, id identity.UserID) (string, error) {
	h, ok := f.passwords[id]
	if !ok {
		return "", identity.ErrUserNotFound
	}
	return h, nil
}
func (f *fakeRepo) AddMembership(context.Context, identity.OrganizationID, identity.UserID, identity.RoleID) error {
	return nil
}
func (f *fakeRepo) PermissionsForUser(context.Context, identity.OrganizationID, identity.UserID) ([]identity.Permission, error) {
	return nil, nil
}
func (f *fakeRepo) BootstrapRoleID(context.Context, identity.OrganizationID) (identity.RoleID, error) {
	return "role-admin", nil
}
func (f *fakeRepo) CreateSession(_ context.Context, s identity.Session, tokenHash []byte) (identity.Session, error) {
	s.ID = identity.SessionID(f.next("sess-"))
	f.sessions[s.ID] = s
	f.byHash[string(tokenHash)] = s.ID
	return s, nil
}
func (f *fakeRepo) GetSessionByTokenHash(_ context.Context, tokenHash []byte) (identity.Session, error) {
	id, ok := f.byHash[string(tokenHash)]
	if !ok {
		return identity.Session{}, identity.ErrSessionNotFound
	}
	return f.sessions[id], nil
}
func (f *fakeRepo) RevokeSession(_ context.Context, id identity.SessionID, evt audit.Event) error {
	s := f.sessions[id]
	now := time.Now()
	s.RevokedAt = &now
	f.sessions[id] = s
	evt.OrganizationID = "org-default"
	f.txEvents = append(f.txEvents, evt)
	return nil
}
func (f *fakeRepo) RevokeUserSessions(_ context.Context, user identity.UserID) error {
	now := time.Now()
	for id, s := range f.sessions {
		if s.UserID == user && s.RevokedAt == nil {
			s.RevokedAt = &now
			f.sessions[id] = s
		}
	}
	return nil
}
func (f *fakeRepo) ValidateSession(_ context.Context, id identity.SessionID) error {
	s, ok := f.sessions[id]
	if !ok || !s.Valid(time.Now()) {
		return identity.ErrSessionNotFound
	}
	return nil
}
func (f *fakeRepo) ExtendSessionIdle(_ context.Context, id identity.SessionID, idle time.Time) error {
	f.extendCalls++
	s := f.sessions[id]
	s.IdleExpiresAt = idle
	f.sessions[id] = s
	return nil
}
func (f *fakeRepo) RotateSession(ctx context.Context, user identity.UserID, s identity.Session, tokenHash []byte, evt audit.Event) (identity.Session, error) {
	_ = f.RevokeUserSessions(ctx, user)
	evt.OrganizationID = "org-default"
	f.txEvents = append(f.txEvents, evt)
	return f.CreateSession(ctx, s, tokenHash)
}
func (f *fakeRepo) LinkIdentityAndRotateSession(ctx context.Context, id identity.OIDCIdentity, s identity.Session, tokenHash []byte, evt audit.Event) (identity.Session, error) {

	key := id.Issuer + "|" + id.Subject
	if existing, ok := f.oidc[key]; ok && existing != id.UserID {
		return identity.Session{}, identity.ErrIdentityLinkedToAnotherUser
	}
	if f.failOIDCComplete {
		return identity.Session{}, errors.New("oidc transaction failed")
	}
	f.oidc[key] = id.UserID
	return f.RotateSession(ctx, id.UserID, s, tokenHash, evt)
}
func (f *fakeRepo) FindUserBySubject(_ context.Context, issuer, subject string) (identity.User, error) {
	id, ok := f.oidc[issuer+"|"+subject]
	if !ok {
		return identity.User{}, identity.ErrNoLinkedAccount
	}
	return f.users[id], nil
}

// linkIdentity is test-fixture setup only. Production linking is exposed only through LinkIdentityAndRotateSession so it cannot bypass its transaction.
func (f *fakeRepo) linkIdentity(id identity.OIDCIdentity) error {
	key := id.Issuer + "|" + id.Subject
	if existing, ok := f.oidc[key]; ok && existing != id.UserID {
		return identity.ErrIdentityLinkedToAnotherUser
	}
	f.oidc[key] = id.UserID
	return nil
}

type fakeHasher struct{}

func (fakeHasher) Hash(_ context.Context, password string) (string, error) {
	return "h:" + password, nil
}
func (fakeHasher) Verify(_ context.Context, password, encoded string) (ok, needsRehash bool, err error) {
	return encoded == "h:"+password, false, nil
}

// errHashHasher fails every Hash, to prove auth.New refuses to build a service whose timing-equalizer hash can't be precomputed.
type errHashHasher struct{ fakeHasher }

func (errHashHasher) Hash(context.Context, string) (string, error) {
	return "", errors.New("argon2 broken")
}

// errVerifyHasher verifies nothing — its errors stand in for an unparsable stored hash, which must not be distinguishable from a wrong password.
type errVerifyHasher struct{ fakeHasher }

func (errVerifyHasher) Verify(context.Context, string, string) (bool, bool, error) {
	return false, false, errors.New("stored hash unparsable")
}

type rehashHasher struct{ failRehash bool }

func (h *rehashHasher) Hash(_ context.Context, password string) (string, error) {
	if h.failRehash {
		return "", errors.New("hash down")
	}
	return "h2:" + password, nil
}

func (h *rehashHasher) Verify(_ context.Context, password, encoded string) (ok, needsRehash bool, err error) {
	ok = encoded == "h2:"+password
	return ok, ok, nil
}

type fakeCSRF struct{}

func (fakeCSRF) Issue(sessionToken string) (string, error) { return "csrf:" + sessionToken, nil }
func (fakeCSRF) Verify(sessionToken, token string) bool    { return token == "csrf:"+sessionToken }

type errCSRF struct{}

func (errCSRF) Issue(string) (string, error) { return "", errors.New("rng failed") }
func (errCSRF) Verify(string, string) bool   { return false }

type capturingRecorder struct {
	mu      sync.Mutex
	events  []audit.Event
	ctxErrs []error
}

func (r *capturingRecorder) Record(ctx context.Context, e audit.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	r.ctxErrs = append(r.ctxErrs, ctx.Err())
	return nil
}

func (r *capturingRecorder) all() []audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]audit.Event(nil), r.events...)
}

type errRecorder struct{}

func (errRecorder) Record(context.Context, audit.Event) error { return errors.New("audit down") }

type ctxAwareRepo struct{ *fakeRepo }

func (r ctxAwareRepo) GetUserForLogin(ctx context.Context, email string) (identity.User, string, identity.LoginBackoff, error) {
	if err := ctx.Err(); err != nil {
		return identity.User{}, "", identity.LoginBackoff{}, err
	}
	return r.fakeRepo.GetUserForLogin(ctx, email)
}

type ctxAwareHasher struct{ fakeHasher }

func (h ctxAwareHasher) Verify(ctx context.Context, password, encoded string) (bool, bool, error) {
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	return h.fakeHasher.Verify(ctx, password, encoded)
}

func newService(t *testing.T, repo auth.Repository) *auth.Service {
	t.Helper()
	return newServiceWithRecorder(t, repo, &capturingRecorder{})
}

func newServiceWithRecorder(t *testing.T, repo auth.Repository, rec auth.AuditRecorder) *auth.Service {
	t.Helper()
	svc, err := auth.New(repo, fakeHasher{}, fakeCSRF{}, rec, auth.Config{})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return svc
}

func TestAuthRecordsAuditEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rec := &capturingRecorder{}
	repo := newFake()
	svc := newServiceWithRecorder(t, repo, rec)

	u, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := svc.Login(ctx, "admin@example.com", "wrong-password-xx"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("wrong-password login = %v", err)
	}
	if _, err := svc.Login(ctx, "ghost@example.com", "hunter2-secretz"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("unknown-email login = %v", err)
	}
	res, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := svc.Logout(ctx, res.Token); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	wantTx := []struct {
		action  audit.Action
		outcome audit.Outcome
	}{
		{audit.ActionAuthBootstrap, audit.OutcomeSucceeded},
		{audit.ActionAuthLogin, audit.OutcomeSucceeded},
		{audit.ActionAuthLogout, audit.OutcomeSucceeded},
	}
	if len(repo.txEvents) != len(wantTx) {
		t.Fatalf("transactional events = %d, want %d: %+v", len(repo.txEvents), len(wantTx), repo.txEvents)
	}
	for idx, w := range wantTx {
		e := repo.txEvents[idx]
		if e.Action != w.action || e.Outcome != w.outcome {
			t.Errorf("txEvent[%d] = %s/%s, want %s/%s", idx, e.Action, e.Outcome, w.action, w.outcome)
		}
	}

	if a := repo.txEvents[1].ActorUserID; a == nil || *a != u.ID {
		t.Errorf("login txEvent actor = %v, want %v", a, u.ID)
	}
	if a := repo.txEvents[2].ActorUserID; a == nil || *a != u.ID {
		t.Errorf("logout txEvent actor = %v, want %v", a, u.ID)
	}

	for _, idx := range []int{1, 2} {
		if tt := repo.txEvents[idx].TargetType; tt != audit.TargetTypeUser {
			t.Errorf("txEvent[%d] TargetType = %q, want %q", idx, tt, audit.TargetTypeUser)
		}
	}

	failures := rec.all()
	if len(failures) != 2 {
		t.Fatalf("recorded %d failure events, want 2: %+v", len(failures), failures)
	}
	for i, e := range failures {
		if e.Action != audit.ActionAuthLogin || e.Outcome != audit.OutcomeFailed {
			t.Errorf("failure[%d] = %s/%s, want AUTH_LOGIN/FAILED", i, e.Action, e.Outcome)
		}
		if e.OrganizationID == "" {
			t.Errorf("failure[%d] has no organization", i)
		}
	}

	if a := failures[0].ActorUserID; a == nil || *a != u.ID {
		t.Errorf("wrong-password failure actor = %v, want %v", a, u.ID)
	}
	if failures[1].ActorUserID != nil {
		t.Errorf("unknown-email failure actor = %v, want nil", *failures[1].ActorUserID)
	}

	if tt := failures[0].TargetType; tt != audit.TargetTypeUser {
		t.Errorf("wrong-password failure TargetType = %q, want %q", tt, audit.TargetTypeUser)
	}
	if tt := failures[1].TargetType; tt != "" {
		t.Errorf("unknown-email failure TargetType = %q, want empty", tt)
	}
}

func TestFailedLoginAuditSurvivesCancelledContext(t *testing.T) {
	t.Parallel()
	rec := &capturingRecorder{}
	svc := newServiceWithRecorder(t, newFake(), rec)

	// A client that disconnects mid-request must not erase the failed-attempt trail: recordAudit detaches from the request ctx (WithoutCancel).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Login(ctx, "ghost@example.com", "hunter2-secretz"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("unknown-email login = %v", err)
	}
	events := rec.all()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(events))
	}
	if err := rec.ctxErrs[0]; err != nil {
		t.Errorf("audit write saw a dead context (%v); recordAudit must detach from cancellation", err)
	}
}

func TestFailedLoginAuditSurvivesCancelDuringLookup(t *testing.T) {
	t.Parallel()
	rec := &capturingRecorder{}
	svc, err := auth.New(ctxAwareRepo{newFake()}, fakeHasher{}, fakeCSRF{}, rec, auth.Config{})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Login = %v, want context.Canceled", err)
	}
	events := rec.all()
	if len(events) != 1 || events[0].Action != audit.ActionAuthLogin || events[0].Outcome != audit.OutcomeFailed {
		t.Fatalf("events = %+v, want one AUTH_LOGIN/FAILED", events)
	}
	if rec.ctxErrs[0] != nil {
		t.Errorf("audit write saw a dead context (%v)", rec.ctxErrs[0])
	}
}

func TestFailedLoginAuditSurvivesCancelDuringVerify(t *testing.T) {
	t.Parallel()
	rec := &capturingRecorder{}
	repo := newFake()
	svc, err := auth.New(repo, ctxAwareHasher{}, fakeCSRF{}, rec, auth.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	u, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := svc.Login(cancelled, "admin@example.com", "hunter2-secretz"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Login = %v, want context.Canceled", err)
	}
	events := rec.all()
	if len(events) != 1 || events[0].Outcome != audit.OutcomeFailed {
		t.Fatalf("events = %+v, want one AUTH_LOGIN/FAILED", events)
	}
	if a := events[0].ActorUserID; a == nil || *a != u.ID {
		t.Errorf("event actor = %v, want %v (user was resolved before the hash)", a, u.ID)
	}
	if rec.ctxErrs[0] != nil {
		t.Errorf("audit write saw a dead context (%v)", rec.ctxErrs[0])
	}
}

func TestAuditFailureDoesNotBlockAuth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newServiceWithRecorder(t, newFake(), errRecorder{})

	// The failure path must succeed even though the audit write fails (best-effort applies to no-state-change events only).
	if _, err := svc.Login(ctx, "ghost@example.com", "some-long-password"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("login with failing audit = %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthenticateIsPureRead_SlideIdleExtends(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	clock := time.Now()
	svc := newServiceWithRecorder(t, repo, &capturingRecorder{}).WithClock(func() time.Time { return clock })

	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	res, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	idle0 := res.Session.IdleExpiresAt

	// Well past IdleRenewInterval — enough that a slide WOULD move the window.
	clock = clock.Add(time.Hour)

	// Authenticate is a pure read: it must NOT advance the idle expiry (so a later CSRF-rejected request can't keep the session alive).
	_, sess, err := svc.Authenticate(ctx, res.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !sess.IdleExpiresAt.Equal(idle0) {
		t.Errorf("Authenticate advanced idle expiry %v → %v; it must be a pure read", idle0, sess.IdleExpiresAt)
	}

	if err := svc.SlideIdle(ctx, sess); err != nil {
		t.Fatalf("SlideIdle: %v", err)
	}
	_, slid, err := svc.Authenticate(ctx, res.Token)
	if err != nil {
		t.Fatalf("Authenticate after slide: %v", err)
	}
	if !slid.IdleExpiresAt.After(idle0) {
		t.Errorf("SlideIdle did not advance idle expiry (%v → %v)", idle0, slid.IdleExpiresAt)
	}
}

func TestNewRefusesBrokenDependencies(t *testing.T) {
	t.Parallel()
	if svc, err := auth.New(nil, fakeHasher{}, fakeCSRF{}, &capturingRecorder{}, auth.Config{}); err == nil || svc != nil {
		t.Errorf("New with nil repo = (%v, %v), want nil service + error", svc, err)
	}
	if svc, err := auth.New(newFake(), errHashHasher{}, fakeCSRF{}, &capturingRecorder{}, auth.Config{}); err == nil || svc != nil {
		t.Errorf("New with failing equalizer hash = (%v, %v), want nil service + error", svc, err)
	}
}

func TestSlideIdleThrottlesFrequentWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	clock := time.Now()
	svc := newServiceWithRecorder(t, repo, &capturingRecorder{}).WithClock(func() time.Time { return clock })

	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz")
	if err != nil {
		t.Fatal(err)
	}

	clock = clock.Add(10 * time.Second)
	if err := svc.SlideIdle(ctx, res.Session); err != nil {
		t.Fatalf("SlideIdle: %v", err)
	}
	if repo.extendCalls != 0 {
		t.Errorf("SlideIdle within the renew interval wrote %d times, want 0", repo.extendCalls)
	}

	clock = clock.Add(2 * time.Minute)
	if err := svc.SlideIdle(ctx, res.Session); err != nil {
		t.Fatalf("SlideIdle: %v", err)
	}
	if repo.extendCalls != 1 {
		t.Errorf("SlideIdle past the renew interval wrote %d times, want 1", repo.extendCalls)
	}
}

func TestSlideIdleRejectsRevokedSessionWithinRenewInterval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	clock := time.Now()
	svc := newServiceWithRecorder(t, repo, &capturingRecorder{}).WithClock(func() time.Time { return clock })
	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz")
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(10 * time.Second)
	if err := repo.RevokeSession(ctx, res.Session.ID, audit.Event{Action: audit.ActionAuthLogout}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SlideIdle(ctx, res.Session); !errors.Is(err, identity.ErrSessionNotFound) {
		t.Fatalf("SlideIdle after revocation = %v, want ErrSessionNotFound", err)
	}
	if repo.extendCalls != 0 {
		t.Errorf("throttled revocation check wrote %d times, want 0", repo.extendCalls)
	}
}

func TestLoginRehashesOutdatedProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	hasher := &rehashHasher{}
	svc, err := auth.New(repo, hasher, fakeCSRF{}, &capturingRecorder{}, auth.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatal(err)
	}
	baseline := repo.setPasswordCalls

	if _, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if repo.setPasswordCalls != baseline+1 {
		t.Errorf("rehash-on-login stored %d times, want exactly 1", repo.setPasswordCalls-baseline)
	}

	hasher.failRehash = true
	if _, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz"); err != nil {
		t.Errorf("Login with failing rehash = %v, want success (best-effort)", err)
	}
	if repo.setPasswordCalls != baseline+1 {
		t.Errorf("failing rehash stored a password anyway (%d writes)", repo.setPasswordCalls-baseline)
	}
}

func TestLoginUniformErrorOnUnverifiableHash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()

	boot := newServiceWithRecorder(t, repo, &capturingRecorder{})
	if _, err := boot.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatal(err)
	}

	rec := &capturingRecorder{}
	svc, err := auth.New(repo, errVerifyHasher{}, fakeCSRF{}, rec, auth.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("login with unverifiable hash = %v, want ErrInvalidCredentials (uniform)", err)
	}
	events := rec.all()
	if len(events) != 1 || events[0].Action != audit.ActionAuthLogin || events[0].Outcome != audit.OutcomeFailed {
		t.Errorf("events = %+v, want one AUTH_LOGIN/FAILED", events)
	}
}

func TestPublicConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	svc := newService(t, repo)

	cfg, err := svc.PublicConfig(ctx)
	if err != nil {
		t.Fatalf("PublicConfig: %v", err)
	}
	if cfg.GoogleEnabled || !cfg.NeedsBootstrap {
		t.Errorf("fresh instance config = %+v, want google off + needs bootstrap", cfg)
	}

	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatal(err)
	}
	svc.WithOIDCProvider(&fakeProvider{})
	cfg, err = svc.PublicConfig(ctx)
	if err != nil {
		t.Fatalf("PublicConfig after bootstrap: %v", err)
	}
	if !cfg.GoogleEnabled || cfg.NeedsBootstrap {
		t.Errorf("configured instance config = %+v, want google on + bootstrapped", cfg)
	}
}

func TestBootstrapOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newService(t, newFake())

	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := svc.Bootstrap(ctx, "second@example.com", "another-secret-pw", "Two"); !errors.Is(err, identity.ErrAlreadyBootstrapped) {
		t.Errorf("second Bootstrap = %v, want ErrAlreadyBootstrapped", err)
	}
}

func TestBootstrapPasswordRoundTripsVerbatim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		password string
	}{
		{"leading space", " hunter2-secretzz"},
		{"trailing space", "hunter2-secretzz "},
		{"internal spaces", "correct horse battery staple"},
		{"tab inside", "hunter2\tsecretzz"},
		{"multi-byte", "비밀번호가-충분히-길어야-합니다"},
		{"emoji", "🙂🙂🙂-hunter2-secretz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := newService(t, newFake())
			email := "admin@example.com"
			if _, err := svc.Bootstrap(ctx, email, tc.password, "Admin"); err != nil {
				t.Fatalf("Bootstrap: %v", err)
			}
			if _, err := svc.Login(ctx, email, tc.password); err != nil {
				t.Errorf("Login with the bootstrapped password failed: %v", err)
			}

			if trimmed := strings.TrimSpace(tc.password); trimmed != tc.password {
				if _, err := svc.Login(ctx, email, trimmed); err == nil {
					t.Error("the trimmed password also signed in — the space was dropped somewhere")
				}
			}
		})
	}
}

func TestBootstrapRefusedRecordsFailureEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rec := &capturingRecorder{}
	svc := newServiceWithRecorder(t, newFake(), rec)

	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatalf("first Bootstrap: %v", err)
	}
	if _, err := svc.Bootstrap(ctx, "attacker@example.com", "another-secret-pw", "X"); !errors.Is(err, identity.ErrAlreadyBootstrapped) {
		t.Fatalf("second Bootstrap = %v, want ErrAlreadyBootstrapped", err)
	}

	var succeeded, failed int
	for _, e := range rec.all() {
		if e.Action != audit.ActionAuthBootstrap {
			continue
		}
		switch e.Outcome {
		case audit.OutcomeSucceeded:
			succeeded++
		case audit.OutcomeFailed:
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("bootstrap FAILED events = %d, want 1 (the refused attempt)", failed)
	}

	if succeeded != 0 {
		t.Errorf("recorder saw %d bootstrap SUCCEEDED events, want 0 (success commits in-tx)", succeeded)
	}
}

func TestBootstrapValidatesInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name, email, password string
		want                  error
	}{
		{"empty email", "", "correct-horse-battery", identity.ErrInvalidEmail},
		{"email without @", "adminexample.com", "correct-horse-battery", identity.ErrInvalidEmail},
		{"double @", "a@@example.com", "correct-horse-battery", identity.ErrInvalidEmail},
		{"leading dot local", ".a@example.com", "correct-horse-battery", identity.ErrInvalidEmail},
		{"hyphen domain label", "a@-x.com", "correct-horse-battery", identity.ErrInvalidEmail},
		{"domain without dot", "a@b", "correct-horse-battery", identity.ErrInvalidEmail},
		{"space in address", "a b@example.com", "correct-horse-battery", identity.ErrInvalidEmail},
		{"empty password", "admin@example.com", "", identity.ErrWeakPassword},
		{"short password", "admin@example.com", "short", identity.ErrWeakPassword},
	}

	dnCases := []struct {
		name, display string
		want          error
	}{
		{"empty", "", identity.ErrInvalidDisplayName},
		{"whitespace only", "   ", identity.ErrInvalidDisplayName},
		{"too long", strings.Repeat("x", 257), identity.ErrInvalidDisplayName},
		{"control char", "Ad\x00min", identity.ErrInvalidDisplayName},
		{"newline", "Ad\nmin", identity.ErrInvalidDisplayName},
		{"rtl override", "Admin\u202egol.tidua", identity.ErrInvalidDisplayName},
		{"zero-width space", "Ad\u200bmin", identity.ErrInvalidDisplayName},
		{"line separator", "Admin\u2028logout", identity.ErrInvalidDisplayName},
		{"paragraph separator", "Admin\u2029logout", identity.ErrInvalidDisplayName},
	}
	for _, tc := range dnCases {
		t.Run("display/"+tc.name, func(t *testing.T) {
			t.Parallel()
			repo := newFake()
			svc := newService(t, repo)
			if _, err := svc.Bootstrap(ctx, "admin@example.com", "correct-horse-battery", tc.display); !errors.Is(err, tc.want) {
				t.Errorf("Bootstrap display = %v, want %v", err, tc.want)
			}
			if len(repo.users) != 0 {
				t.Errorf("invalid display name created a user")
			}
		})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := newFake()
			svc := newService(t, repo)
			if _, err := svc.Bootstrap(ctx, tc.email, tc.password, "Admin"); !errors.Is(err, tc.want) {
				t.Errorf("Bootstrap = %v, want %v", err, tc.want)
			}
			if len(repo.users) != 0 {
				t.Errorf("invalid bootstrap created %d users, want 0", len(repo.users))
			}
		})
	}
}

func TestLoginRejectsWrongPasswordAndDisabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	svc := newService(t, repo)
	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Login(ctx, "admin@example.com", "wrong"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("wrong password = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Login(ctx, "nobody@example.com", "x"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("unknown user = %v, want ErrInvalidCredentials", err)
	}

	id := repo.emails["admin@example.com"]
	u := repo.users[id]
	u.Status = identity.StatusDisabled
	repo.users[id] = u
	if _, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("disabled login = %v, want ErrInvalidCredentials", err)
	}
}

type countingHasher struct {
	fakeHasher
	verifies atomic.Int64
}

func (h *countingHasher) Verify(ctx context.Context, password, encoded string) (bool, bool, error) {
	h.verifies.Add(1)
	return h.fakeHasher.Verify(ctx, password, encoded)
}

func TestLoginRejectsOversizedInputBeforeHashing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	hasher := &countingHasher{}
	svc, err := auth.New(repo, hasher, fakeCSRF{}, &capturingRecorder{}, auth.Config{})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatal(err)
	}
	hasher.verifies.Store(0)

	if _, err := svc.Login(ctx, "admin@example.com", strings.Repeat("x", 2000)); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("oversized password = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Login(ctx, strings.Repeat("a", 300)+"@example.com", "hunter2-secretz"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("oversized email = %v, want ErrInvalidCredentials", err)
	}
	if n := hasher.verifies.Load(); n != 0 {
		t.Errorf("oversized input reached the hasher %d times, want 0", n)
	}

	// The caps must not break a real login (regression guard).
	if _, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz"); err != nil {
		t.Errorf("normal login after cap checks: %v", err)
	}
}

func TestLoginCanonicalizesEmailAndPassword(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newService(t, newFake())

	const bootPw = "café-password-xy"
	if _, err := svc.Bootstrap(ctx, "Admin@Example.com", bootPw, "Admin"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	// A differently-cased email and the precomposed é must still authenticate.
	const loginPw = "café-password-xy"
	if _, err := svc.Login(ctx, "ADMIN@EXAMPLE.COM", loginPw); err != nil {
		t.Errorf("case-folded email + NFC-equivalent password should log in: %v", err)
	}
}

func TestLoginLogoutAuthenticate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newService(t, newFake())
	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if res.Token == "" || res.CSRF == "" {
		t.Fatal("login should return a token and a CSRF token")
	}

	u, _, err := svc.Authenticate(ctx, res.Token)
	if err != nil || u.Email != "admin@example.com" {
		t.Fatalf("Authenticate = %v, %v", u, err)
	}

	if !svc.VerifyCSRF(res.Token, res.CSRF) {
		t.Error("VerifyCSRF should accept the issued token")
	}
	if svc.VerifyCSRF(res.Token, "forged") {
		t.Error("VerifyCSRF should reject a forged token")
	}
	// Contract pin: CSRF binds the raw session TOKEN, not the session id — a transport interceptor must pass the cookie value, not Session.ID, or every check fails.
	if svc.VerifyCSRF(string(res.Session.ID), res.CSRF) {
		t.Error("VerifyCSRF must be keyed by the session token, not the session id")
	}

	if err := svc.Logout(ctx, res.Token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, res.Token); err == nil {
		t.Error("Authenticate should fail after logout")
	}
}

func TestAuthenticateRejectsExpired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newService(t, newFake())
	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz")
	if err != nil {
		t.Fatal(err)
	}

	// Advance the clock past the absolute expiry.
	future := svc.WithClock(func() time.Time { return time.Now().Add(8 * 24 * time.Hour) })
	if _, _, err := future.Authenticate(ctx, res.Token); err == nil {
		t.Error("Authenticate should reject an expired session")
	}
}

func TestSlideIdleCapsAtAbsolute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newService(t, newFake())
	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz")
	if err != nil {
		t.Fatal(err)
	}

	// Sliding near the absolute expiry (default idle 12h < absolute 7d, so jump to 1h before absolute) must clamp the new idle expiry at the absolute one.
	near := res.Session.AbsoluteExpiresAt.Add(-time.Hour)
	later := svc.WithClock(func() time.Time { return near })
	if err := later.SlideIdle(ctx, res.Session); err != nil {
		t.Fatalf("SlideIdle: %v", err)
	}
	_, sess, err := later.Authenticate(ctx, res.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if sess.IdleExpiresAt.After(sess.AbsoluteExpiresAt) {
		t.Errorf("idle expiry %v must not exceed absolute %v", sess.IdleExpiresAt, sess.AbsoluteExpiresAt)
	}
	if !sess.IdleExpiresAt.Equal(sess.AbsoluteExpiresAt) {
		t.Errorf("near absolute, idle should clamp to absolute: idle %v, absolute %v", sess.IdleExpiresAt, sess.AbsoluteExpiresAt)
	}
}

func TestLoginRevokesPriorSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newService(t, newFake())
	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatal(err)
	}

	first, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz")
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := svc.Authenticate(ctx, first.Token); err == nil {
		t.Error("the first session should be revoked after a second login")
	}
	if _, _, err := svc.Authenticate(ctx, second.Token); err != nil {
		t.Errorf("the newest session should authenticate: %v", err)
	}
}

func TestLinkIdentityFixtureRejectsConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	a, _ := repo.CreateUser(ctx, "a@example.com", "A")
	b, _ := repo.CreateUser(ctx, "b@example.com", "B")

	const iss, sub = "https://accounts.google.com", "sub-1"
	if err := repo.linkIdentity(identity.OIDCIdentity{UserID: a.ID, Issuer: iss, Subject: sub}); err != nil {
		t.Fatalf("first link: %v", err)
	}

	if err := repo.linkIdentity(identity.OIDCIdentity{UserID: a.ID, Issuer: iss, Subject: sub}); err != nil {
		t.Errorf("re-link to same user should succeed: %v", err)
	}
	// Same identity, different user — must be rejected, not silently accepted.
	if err := repo.linkIdentity(identity.OIDCIdentity{UserID: b.ID, Issuer: iss, Subject: sub}); !errors.Is(err, identity.ErrIdentityLinkedToAnotherUser) {
		t.Errorf("re-pointing to another user = %v, want ErrIdentityLinkedToAnotherUser", err)
	}
}

func TestLoginCSRFFailureLeavesPriorSessionIntact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	svc := newService(t, repo)
	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2-secretz", "Admin"); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz")
	if err != nil {
		t.Fatal(err)
	}

	// A service whose CSRF issuance fails must error before RotateSession, so the commit is the last fallible step and the prior session is not revoked.
	broken, err := auth.New(repo, fakeHasher{}, errCSRF{}, &capturingRecorder{}, auth.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broken.Login(ctx, "admin@example.com", "hunter2-secretz"); err == nil {
		t.Error("Login should fail when CSRF issuance fails")
	}
	if _, _, err := svc.Authenticate(ctx, first.Token); err != nil {
		t.Errorf("the prior session must survive a failed login: %v", err)
	}
}
