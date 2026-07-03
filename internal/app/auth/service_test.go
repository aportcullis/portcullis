package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// --- in-memory fake satisfying auth.Repository ---

type fakeRepo struct {
	users     map[identity.UserID]identity.User
	emails    map[string]identity.UserID
	passwords map[identity.UserID]string
	sessions  map[identity.SessionID]identity.Session
	byHash    map[string]identity.SessionID
	oidc      map[string]identity.UserID
	seq       int
	// txEvents captures the audit events passed to the transactional ops
	// (BootstrapAdmin, RotateSession, RevokeSession) in call order, mimicking the
	// real store's same-transaction write (ADR-0009). The fake completes the
	// actor for bootstrap the way the store does.
	txEvents []audit.Event
}

func newFake() *fakeRepo {
	return &fakeRepo{
		users:     map[identity.UserID]identity.User{},
		emails:    map[string]identity.UserID{},
		passwords: map[identity.UserID]string{},
		sessions:  map[identity.SessionID]identity.Session{},
		byHash:    map[string]identity.SessionID{},
		oidc:      map[string]identity.UserID{},
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
func (f *fakeRepo) GetUserForLogin(_ context.Context, email string) (identity.User, string, error) {
	id, ok := f.emails[email]
	if !ok {
		return identity.User{}, "", identity.ErrUserNotFound
	}
	return f.users[id], f.passwords[id], nil
}
func (f *fakeRepo) GetUserByID(_ context.Context, id identity.UserID) (identity.User, error) {
	u, ok := f.users[id]
	if !ok {
		return identity.User{}, identity.ErrUserNotFound
	}
	return u, nil
}
func (f *fakeRepo) SetPassword(_ context.Context, id identity.UserID, phc string) error {
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
func (f *fakeRepo) ExtendSessionIdle(_ context.Context, id identity.SessionID, idle time.Time) error {
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
func (f *fakeRepo) FindUserBySubject(_ context.Context, issuer, subject string) (identity.User, error) {
	id, ok := f.oidc[issuer+"|"+subject]
	if !ok {
		return identity.User{}, identity.ErrNoLinkedAccount
	}
	return f.users[id], nil
}
func (f *fakeRepo) LinkIdentity(_ context.Context, id identity.OIDCIdentity) error {
	key := id.Issuer + "|" + id.Subject
	if existing, ok := f.oidc[key]; ok && existing != id.UserID {
		return identity.ErrIdentityLinkedToAnotherUser
	}
	f.oidc[key] = id.UserID
	return nil
}

// --- fake crypto adapters (fast: no real Argon2/keyring) ---

type fakeHasher struct{}

func (fakeHasher) Hash(_ context.Context, password string) (string, error) {
	return "h:" + password, nil
}
func (fakeHasher) Verify(_ context.Context, password, encoded string) (ok, needsRehash bool, err error) {
	return encoded == "h:"+password, false, nil
}

type fakeCSRF struct{}

func (fakeCSRF) Issue(sessionToken string) (string, error) { return "csrf:" + sessionToken, nil }
func (fakeCSRF) Verify(sessionToken, token string) bool    { return token == "csrf:"+sessionToken }

// errCSRF fails issuance, to prove a CSRF failure happens before the rotation commit.
type errCSRF struct{}

func (errCSRF) Issue(string) (string, error) { return "", errors.New("rng failed") }
func (errCSRF) Verify(string, string) bool   { return false }

// capturingRecorder collects audit events so tests can assert on emission,
// and the ctx liveness observed at each write (to pin the WithoutCancel behavior).
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

// errRecorder always fails, to prove audit is best-effort.
type errRecorder struct{}

func (errRecorder) Record(context.Context, audit.Event) error { return errors.New("audit down") }

// ctxAwareRepo respects request-context cancellation on the login lookup, the
// way the real pgx-backed store does — the plain fake ignores ctx.
type ctxAwareRepo struct{ *fakeRepo }

func (r ctxAwareRepo) GetUserForLogin(ctx context.Context, email string) (identity.User, string, error) {
	if err := ctx.Err(); err != nil {
		return identity.User{}, "", err
	}
	return r.fakeRepo.GetUserForLogin(ctx, email)
}

// ctxAwareHasher respects cancellation the way the bounded Argon2 hasher does
// (its semaphore acquire returns ctx.Err()).
type ctxAwareHasher struct{ fakeHasher }

func (h ctxAwareHasher) Verify(ctx context.Context, password, encoded string) (bool, bool, error) {
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	return h.fakeHasher.Verify(ctx, password, encoded)
}

// --- helpers ---

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

// --- tests ---

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

	// State-changing ops ride the repo call so they commit atomically (ADR-0009).
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
	for i, w := range wantTx {
		e := repo.txEvents[i]
		if e.Action != w.action || e.Outcome != w.outcome {
			t.Errorf("txEvent[%d] = %s/%s, want %s/%s", i, e.Action, e.Outcome, w.action, w.outcome)
		}
	}
	// Login/logout events carry the resolved actor; bootstrap's actor is completed
	// by the store inside the transaction.
	if a := repo.txEvents[1].ActorUserID; a == nil || *a != u.ID {
		t.Errorf("login txEvent actor = %v, want %v", a, u.ID)
	}
	if a := repo.txEvents[2].ActorUserID; a == nil || *a != u.ID {
		t.Errorf("logout txEvent actor = %v, want %v", a, u.ID)
	}

	// Failures change no state, so they go through the best-effort recorder.
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
	// Wrong password resolved the user; unknown email has no actor to attribute.
	if a := failures[0].ActorUserID; a == nil || *a != u.ID {
		t.Errorf("wrong-password failure actor = %v, want %v", a, u.ID)
	}
	if failures[1].ActorUserID != nil {
		t.Errorf("unknown-email failure actor = %v, want nil", *failures[1].ActorUserID)
	}
}

func TestFailedLoginAuditSurvivesCancelledContext(t *testing.T) {
	t.Parallel()
	rec := &capturingRecorder{}
	svc := newServiceWithRecorder(t, newFake(), rec)

	// A client that disconnects mid-request must not erase the failed-attempt
	// trail: recordAudit detaches from the request ctx (WithoutCancel).
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

// A disconnect that cancels the request BEFORE the user lookup completes (the
// real store propagates ctx cancellation) must still leave a failed-attempt
// event — otherwise cancelling early erases the trail entirely.
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

// Cancellation while waiting on the (bounded) password hash must likewise still
// record the attempt, attributed to the resolved user.
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

	// The failure path must succeed even though the audit write fails
	// (best-effort applies to no-state-change events only).
	if _, err := svc.Login(ctx, "ghost@example.com", "some-long-password"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("login with failing audit = %v, want ErrInvalidCredentials", err)
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

	// A disabled account is refused with the SAME generic error as a wrong password
	// (OWASP anti-enumeration; ErrUserDisabled is not exposed by the login path).
	id := repo.emails["admin@example.com"]
	u := repo.users[id]
	u.Status = identity.StatusDisabled
	repo.users[id] = u
	if _, err := svc.Login(ctx, "admin@example.com", "hunter2-secretz"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("disabled login = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginCanonicalizesEmailAndPassword(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newService(t, newFake())

	// Bootstrap with a mixed-case email and a decomposed é (e + combining acute).
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

	// CSRF token round-trips (bound to the raw session token) and rejects a wrong value.
	if !svc.VerifyCSRF(res.Token, res.CSRF) {
		t.Error("VerifyCSRF should accept the issued token")
	}
	if svc.VerifyCSRF(res.Token, "forged") {
		t.Error("VerifyCSRF should reject a forged token")
	}
	// Contract pin: CSRF binds the raw session TOKEN, not the session id — a transport
	// interceptor must pass the cookie value, not Session.ID, or every check fails.
	if svc.VerifyCSRF(string(res.Session.ID), res.CSRF) {
		t.Error("VerifyCSRF must be keyed by the session token, not the session id")
	}

	// After logout the session no longer authenticates.
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

func TestAuthenticateSlidesIdle(t *testing.T) {
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
	origIdle := res.Session.IdleExpiresAt

	// Authenticating later slides the idle window forward, capped at absolute.
	later := svc.WithClock(func() time.Time { return time.Now().Add(3 * time.Hour) })
	_, sess, err := later.Authenticate(ctx, res.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !sess.IdleExpiresAt.After(origIdle) {
		t.Errorf("idle expiry should slide forward: orig %v, got %v", origIdle, sess.IdleExpiresAt)
	}
	if sess.IdleExpiresAt.After(sess.AbsoluteExpiresAt) {
		t.Error("idle expiry must not exceed absolute expiry")
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

	// ADR-0006: a new login invalidates the prior session.
	if _, _, err := svc.Authenticate(ctx, first.Token); err == nil {
		t.Error("the first session should be revoked after a second login")
	}
	if _, _, err := svc.Authenticate(ctx, second.Token); err != nil {
		t.Errorf("the newest session should authenticate: %v", err)
	}
}

func TestLinkIdentityConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	a, _ := repo.CreateUser(ctx, "a@example.com", "A")
	b, _ := repo.CreateUser(ctx, "b@example.com", "B")

	const iss, sub = "https://accounts.google.com", "sub-1"
	if err := repo.LinkIdentity(ctx, identity.OIDCIdentity{UserID: a.ID, Issuer: iss, Subject: sub}); err != nil {
		t.Fatalf("first link: %v", err)
	}
	// Same identity, same user — idempotent.
	if err := repo.LinkIdentity(ctx, identity.OIDCIdentity{UserID: a.ID, Issuer: iss, Subject: sub}); err != nil {
		t.Errorf("re-link to same user should succeed: %v", err)
	}
	// Same identity, different user — must be rejected, not silently accepted.
	if err := repo.LinkIdentity(ctx, identity.OIDCIdentity{UserID: b.ID, Issuer: iss, Subject: sub}); !errors.Is(err, identity.ErrIdentityLinkedToAnotherUser) {
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

	// A service whose CSRF issuance fails must error before RotateSession, so the
	// commit is the last fallible step and the prior session is not revoked.
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
