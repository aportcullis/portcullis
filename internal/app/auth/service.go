// Package auth is the application service for authentication: password login,
// server-side sessions, CSRF tokens, and first-run bootstrap. It orchestrates
// the identity domain ports and the crypto package; it holds no transport or
// storage detail of its own.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
)

// Repository is the storage surface the auth service needs; the postgres
// IdentityStore satisfies it, and tests use an in-memory fake.
type Repository interface {
	identity.UserRepository
	identity.RoleRepository
	identity.SessionRepository
	identity.OIDCRepository
	// BootstrapAdmin atomically creates the first admin (user + password +
	// bootstrap-role membership) under a lock, returning ErrAlreadyBootstrapped if
	// any user already exists. The password is pre-hashed by the caller.
	BootstrapAdmin(ctx context.Context, email, displayName, passwordHash string) (identity.User, error)
}

// Config tunes session lifetimes and the password-hash profile.
type Config struct {
	Idle     time.Duration
	Absolute time.Duration
	// IdleRenewInterval throttles idle-window writes: the session is only
	// re-persisted once activity has advanced the window by at least this much,
	// so high-traffic sessions don't write on every request.
	IdleRenewInterval time.Duration
	Argon2            crypto.Argon2Params
}

// Service implements the auth use cases.
type Service struct {
	repo      Repository
	keyring   *crypto.Keyring
	now       func() time.Time
	idle      time.Duration
	absolute  time.Duration
	idleRenew time.Duration
	argon2    crypto.Argon2Params
}

// New builds the service with sensible defaults (12h idle, 7d absolute, idle
// writes throttled to once a minute, the default Argon2id profile).
func New(repo Repository, kr *crypto.Keyring, cfg Config) *Service {
	if cfg.Idle == 0 {
		cfg.Idle = 12 * time.Hour
	}
	if cfg.Absolute == 0 {
		cfg.Absolute = 7 * 24 * time.Hour
	}
	if cfg.IdleRenewInterval == 0 {
		cfg.IdleRenewInterval = time.Minute
	}
	if cfg.Argon2 == (crypto.Argon2Params{}) {
		cfg.Argon2 = crypto.DefaultArgon2Params
	}
	return &Service{repo: repo, keyring: kr, now: time.Now, idle: cfg.Idle, absolute: cfg.Absolute, idleRenew: cfg.IdleRenewInterval, argon2: cfg.Argon2}
}

// WithClock overrides the time source (tests).
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

// Session is the result of a successful login: the persisted session plus the
// raw token (for the session cookie) and the CSRF token (for the readable cookie).
type Session struct {
	User    identity.User
	Session identity.Session
	Token   string
	CSRF    string
}

// Bootstrap creates the first admin. It is refused once any user exists. The
// password is hashed before the transaction; the repository re-checks the
// no-user invariant under a lock and writes user + password + membership
// atomically (so a partial failure can't leave a half-created admin that
// permanently blocks bootstrap).
func (s *Service) Bootstrap(ctx context.Context, email, password, displayName string) (identity.User, error) {
	// Fast path: skip the expensive hash if already bootstrapped (the repository
	// re-checks authoritatively under the lock).
	n, err := s.repo.CountUsers(ctx)
	if err != nil {
		return identity.User{}, err
	}
	if n > 0 {
		return identity.User{}, identity.ErrAlreadyBootstrapped
	}
	phc, err := crypto.HashPassword(password, s.argon2)
	if err != nil {
		return identity.User{}, err
	}
	return s.repo.BootstrapAdmin(ctx, email, displayName, phc)
}

// Login verifies the password and issues a new session (rotation on every login).
func (s *Service) Login(ctx context.Context, email, password string) (Session, error) {
	u, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return Session{}, identity.ErrInvalidCredentials
		}
		return Session{}, err
	}
	if !u.Active() {
		return Session{}, identity.ErrUserDisabled
	}

	hash, err := s.repo.GetPasswordHash(ctx, u.ID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return Session{}, identity.ErrInvalidCredentials
		}
		return Session{}, err
	}
	ok, needsRehash, err := crypto.VerifyPassword(password, hash, s.argon2)
	if err != nil {
		return Session{}, err
	}
	if !ok {
		return Session{}, identity.ErrInvalidCredentials
	}
	if needsRehash {
		if nh, herr := crypto.HashPassword(password, s.argon2); herr == nil {
			_ = s.repo.SetPassword(ctx, u.ID, nh)
		}
	}
	return s.issueSession(ctx, u)
}

// Logout revokes the session for the given raw token (a no-op if unknown).
func (s *Service) Logout(ctx context.Context, token string) error {
	sum := sha256.Sum256([]byte(token))
	sess, err := s.repo.GetSessionByTokenHash(ctx, sum[:])
	if err != nil {
		if errors.Is(err, identity.ErrSessionNotFound) {
			return nil
		}
		return err
	}
	return s.repo.RevokeSession(ctx, sess.ID)
}

// Authenticate resolves the user for a raw session token, rejecting revoked,
// expired, or disabled sessions/users.
func (s *Service) Authenticate(ctx context.Context, token string) (identity.User, identity.Session, error) {
	sum := sha256.Sum256([]byte(token))
	sess, err := s.repo.GetSessionByTokenHash(ctx, sum[:])
	if err != nil {
		return identity.User{}, identity.Session{}, err
	}
	now := s.now()
	if !sess.Valid(now) {
		return identity.User{}, identity.Session{}, identity.ErrSessionNotFound
	}
	u, err := s.repo.GetUserByID(ctx, sess.UserID)
	if err != nil {
		return identity.User{}, identity.Session{}, err
	}
	if !u.Active() {
		return identity.User{}, identity.Session{}, identity.ErrUserDisabled
	}

	// Slide the idle window forward on activity, capped at the absolute expiry.
	// Throttle the write to once per IdleRenewInterval so a busy session doesn't
	// UPDATE on every request; the store uses greatest() so a late-arriving older
	// request can't move the expiry backward.
	newIdle := now.Add(s.idle)
	if newIdle.After(sess.AbsoluteExpiresAt) {
		newIdle = sess.AbsoluteExpiresAt
	}
	if newIdle.Sub(sess.IdleExpiresAt) >= s.idleRenew {
		if err := s.repo.ExtendSessionIdle(ctx, sess.ID, newIdle); err != nil {
			return identity.User{}, identity.Session{}, err
		}
		sess.IdleExpiresAt = newIdle
	}
	return u, sess, nil
}

// CSRFToken returns the HMAC-bound, session-scoped CSRF token (ADR-0006).
func (s *Service) CSRFToken(sessionID identity.SessionID) string {
	d, _ := s.keyring.Digest([]byte("csrf:" + string(sessionID)))
	return base64.RawURLEncoding.EncodeToString(d.Sum)
}

// VerifyCSRF reports whether token is the valid CSRF token for the session.
func (s *Service) VerifyCSRF(sessionID identity.SessionID, token string) bool {
	expected := s.CSRFToken(sessionID)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1
}

func (s *Service) issueSession(ctx context.Context, u identity.User) (Session, error) {
	raw, err := newToken()
	if err != nil {
		return Session{}, err
	}
	sum := sha256.Sum256([]byte(raw))
	sess := identity.NewSession("", u.ID, s.now(), s.idle, s.absolute)
	created, err := s.repo.CreateSession(ctx, sess, sum[:])
	if err != nil {
		return Session{}, err
	}
	return Session{User: u, Session: created, Token: raw, CSRF: s.CSRFToken(created.ID)}, nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
