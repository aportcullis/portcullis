package auth_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
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
func (f *fakeRepo) BootstrapAdmin(ctx context.Context, email, displayName, passwordHash string) (identity.User, error) {
	if len(f.users) > 0 {
		return identity.User{}, identity.ErrAlreadyBootstrapped
	}
	u, err := f.CreateUser(ctx, email, displayName)
	if err != nil {
		return identity.User{}, err
	}
	f.passwords[u.ID] = passwordHash
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
func (f *fakeRepo) PermissionsForUser(context.Context, identity.UserID) ([]identity.Permission, error) {
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
func (f *fakeRepo) RevokeSession(_ context.Context, id identity.SessionID) error {
	s := f.sessions[id]
	now := time.Now()
	s.RevokedAt = &now
	f.sessions[id] = s
	return nil
}
func (f *fakeRepo) ExtendSessionIdle(_ context.Context, id identity.SessionID, idle time.Time) error {
	s := f.sessions[id]
	s.IdleExpiresAt = idle
	f.sessions[id] = s
	return nil
}
func (f *fakeRepo) FindUserBySubject(_ context.Context, issuer, subject string) (identity.User, error) {
	id, ok := f.oidc[issuer+"|"+subject]
	if !ok {
		return identity.User{}, identity.ErrNoLinkedAccount
	}
	return f.users[id], nil
}
func (f *fakeRepo) LinkIdentity(_ context.Context, id identity.OIDCIdentity) error {
	f.oidc[id.Issuer+"|"+id.Subject] = id.UserID
	return nil
}

// --- helpers ---

func newService(t *testing.T, repo auth.Repository) *auth.Service {
	t.Helper()
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	kr, err := crypto.LoadKeyring(base64.StdEncoding.EncodeToString(raw), "")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	weak := crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}
	return auth.New(repo, kr, auth.Config{Argon2: weak})
}

// --- tests ---

func TestBootstrapOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newService(t, newFake())

	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2", "Admin"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := svc.Bootstrap(ctx, "second@example.com", "pw", "Two"); !errors.Is(err, identity.ErrAlreadyBootstrapped) {
		t.Errorf("second Bootstrap = %v, want ErrAlreadyBootstrapped", err)
	}
}

func TestLoginRejectsWrongPasswordAndDisabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	svc := newService(t, repo)
	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2", "Admin"); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Login(ctx, "admin@example.com", "wrong"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("wrong password = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Login(ctx, "nobody@example.com", "x"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("unknown user = %v, want ErrInvalidCredentials", err)
	}

	// Disable the user and confirm login is refused.
	id := repo.emails["admin@example.com"]
	u := repo.users[id]
	u.Status = identity.StatusDisabled
	repo.users[id] = u
	if _, err := svc.Login(ctx, "admin@example.com", "hunter2"); !errors.Is(err, identity.ErrUserDisabled) {
		t.Errorf("disabled login = %v, want ErrUserDisabled", err)
	}
}

func TestLoginLogoutAuthenticate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newService(t, newFake())
	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2", "Admin"); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Login(ctx, "admin@example.com", "hunter2")
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

	// CSRF token round-trips and rejects a wrong value.
	if !svc.VerifyCSRF(res.Session.ID, res.CSRF) {
		t.Error("VerifyCSRF should accept the issued token")
	}
	if svc.VerifyCSRF(res.Session.ID, "forged") {
		t.Error("VerifyCSRF should reject a forged token")
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
	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2", "Admin"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Login(ctx, "admin@example.com", "hunter2")
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
	if _, err := svc.Bootstrap(ctx, "admin@example.com", "hunter2", "Admin"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Login(ctx, "admin@example.com", "hunter2")
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
