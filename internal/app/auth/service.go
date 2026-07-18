// Package auth is the application service for authentication: password login,
// server-side sessions, CSRF tokens, and first-run bootstrap. It orchestrates
// the identity domain ports and an injected PasswordHasher/Signer; it holds no
// transport, storage, or crypto detail of its own.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/platform/logging"
	"github.com/aportcullis/portcullis/internal/platform/reqmeta"
)

// New builds the service with sensible defaults (12h idle, 7d absolute, idle
// writes throttled to once a minute). The password hasher, CSRF protector, and
// audit recorder are injected (see ports.go) so crypto and persistence stay
// infrastructure details. It fails if a dependency is nil or the password
// profile can't produce the timing-equalizer hash (e.g. an invalid Argon2
// config), so a half-built service never starts.
func New(repo Repository, hasher PasswordHasher, csrf CSRFProtector, auditor AuditRecorder, cfg Config) (*Service, error) {
	if repo == nil || hasher == nil || csrf == nil || auditor == nil {
		return nil, errors.New("auth: nil dependency (repo, hasher, csrf, and auditor are required)")
	}
	if cfg.Idle == 0 {
		cfg.Idle = 12 * time.Hour
	}
	if cfg.Absolute == 0 {
		cfg.Absolute = 7 * 24 * time.Hour
	}
	if cfg.IdleRenewInterval == 0 {
		cfg.IdleRenewInterval = time.Minute
	}
	if cfg.BackoffThreshold == 0 {
		cfg.BackoffThreshold = defaultBackoffThreshold
	}
	if cfg.BackoffBase == 0 {
		cfg.BackoffBase = defaultBackoffBase
	}
	if cfg.BackoffCap == 0 {
		cfg.BackoffCap = defaultBackoffCap
	}
	if cfg.BackoffThreshold < 0 || cfg.BackoffBase < 0 || cfg.BackoffCap < cfg.BackoffBase {
		return nil, fmt.Errorf("auth: invalid backoff config (threshold %d, base %s, cap %s)", cfg.BackoffThreshold, cfg.BackoffBase, cfg.BackoffCap)
	}
	// Precompute a hash so a login for an unknown account spends the same hashing
	// time as a real one, closing the account-enumeration timing side channel.
	dummy, err := hasher.Hash(context.Background(), "portcullis-login-timing-equalizer")
	if err != nil {
		return nil, fmt.Errorf("auth: precompute timing-equalizer hash: %w", err)
	}
	return &Service{
		repo: repo, hasher: hasher, csrf: csrf, auditor: auditor,
		logger: slog.Default(), now: time.Now,
		idle: cfg.Idle, absolute: cfg.Absolute, idleRenew: cfg.IdleRenewInterval, dummyHash: dummy,
		backoffThreshold: cfg.BackoffThreshold, backoffBase: cfg.BackoffBase, backoffCap: cfg.BackoffCap,
		jitter: cryptoJitter,
	}, nil
}

// WithClock overrides the time source (tests).
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

// WithJitter overrides the [0,1) sample source behind the lockout jitter
// (tests pin it for exact window assertions). Returns the service for chaining.
func (s *Service) WithJitter(j func() float64) *Service {
	if j != nil {
		s.jitter = j
	}
	return s
}

// cryptoJitter draws a uniform [0,1) sample from crypto/rand (53 mantissa
// bits). The jitter exists so lockout expiries can't be probed exactly
// (ADR-0006); on the never-expected rand failure it degrades to mid-window
// rather than a predictable edge.
func cryptoJitter() float64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0.5
	}
	return float64(binary.BigEndian.Uint64(b[:])>>11) / (1 << 53)
}

// WithLogger overrides the logger that records audit-write failures (defaults to
// slog.Default()). Returns the service for chaining.
func (s *Service) WithLogger(l *slog.Logger) *Service {
	if l != nil {
		s.logger = l
	}
	return s
}

// newEvent builds the request-scoped envelope of an auth audit event — a value
// object, assembled (not injected: DI is for behavioral collaborators, i.e. the
// ports). It lives in the app layer because extracting RequestID/SourceIP from
// the context is request handling, not domain logic. The repository completes
// OrganizationID (and, for bootstrap, the actor + target); for state-changing ops
// the event rides the repo call and commits in the same transaction (ADR-0009).
// TargetType is left empty here and set only when a target user is resolved (see
// withActor / the store's BootstrapAdmin), so an actor-less event — a failed
// login for an unknown email — carries no spurious "user" target.
func newEvent(ctx context.Context, action audit.Action, outcome audit.Outcome) audit.Event {
	return audit.Event{
		ActorType: audit.ActorUser,
		Action:    action,
		Outcome:   outcome,
		RequestID: logging.RequestID(ctx),
		SourceIP:  reqmeta.ClientIP(ctx),
	}
}

// withActor attributes an event to a resolved user, tagging it as targeting that
// user (TargetType is set with TargetID so the pair stays consistent — an event
// has a target type iff it has a target).
func withActor(e audit.Event, id identity.UserID) audit.Event {
	e.ActorUserID = &id
	e.TargetType = audit.TargetTypeUser
	e.TargetID = string(id)
	return e
}

// recordAudit persists a no-state-change event (e.g. a failed login),
// best-effort: authentication must not fail because the audit store is briefly
// unavailable, so an error is logged (fields only — never credentials) and
// swallowed (ADR-0009). The write detaches from the request context so a client
// disconnect can't erase the trail, bounded by auditWriteTimeout.
func (s *Service) recordAudit(ctx context.Context, e audit.Event) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedWriteTimeout)
	defer cancel()
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "audit event dropped", "action", e.Action, "reason", "resolve organization", "error_type", fmt.Sprintf("%T", err))
		return
	}
	e.OrganizationID = org
	if err := s.auditor.Record(ctx, e); err != nil {
		s.logger.WarnContext(ctx, "audit event dropped", "action", e.Action, "reason", "record", "error_type", fmt.Sprintf("%T", err))
	}
}

// Bootstrap creates the first admin. It is refused once any user exists. The
// password is hashed before the transaction; the repository re-checks the
// no-user invariant under a lock and writes user + password + membership
// atomically (so a partial failure can't leave a half-created admin that
// permanently blocks bootstrap).
func (s *Service) Bootstrap(ctx context.Context, email, password, displayName string) (_ identity.User, err error) {
	// EVERY failure exit records one best-effort event (as Login does), so probing
	// the public bootstrap endpoint on an already-installed instance leaves a trail.
	// The success path writes its own event in the creation transaction (below), so
	// this fires only on a failed/refused attempt. There is no actor to attribute
	// (the request either predates any user or is refused before one is created).
	failed := newEvent(ctx, audit.ActionAuthBootstrap, audit.OutcomeFailed)
	defer func() {
		if err != nil {
			s.recordAudit(ctx, failed)
		}
	}()
	// Validate before any DB work or hashing so a malformed request is cheap to
	// reject and can't create an admin with an unusable credential.
	email = identity.NormalizeEmail(email)
	if err := identity.ValidateEmail(email); err != nil {
		return identity.User{}, err
	}
	password = normalizePassword(password)
	if err := validatePassword(password); err != nil {
		return identity.User{}, err
	}
	if err := validateDisplayName(displayName); err != nil {
		return identity.User{}, err
	}
	// Fast path: skip the expensive hash if already bootstrapped (the repository
	// re-checks authoritatively under the lock).
	n, err := s.repo.CountUsers(ctx)
	if err != nil {
		return identity.User{}, err
	}
	if n > 0 {
		return identity.User{}, identity.ErrAlreadyBootstrapped
	}
	phc, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return identity.User{}, err
	}
	// The audit event commits in the same transaction as the admin creation; the
	// store completes the actor (the created user's id exists only inside the tx).
	return s.repo.BootstrapAdmin(ctx, email, displayName, phc,
		newEvent(ctx, audit.ActionAuthBootstrap, audit.OutcomeSucceeded))
}

// Login verifies the password and issues a new session (rotation on every login).
func (s *Service) Login(ctx context.Context, email, password string) (_ Session, err error) {
	// Canonicalize to match how the credential was stored (case-folded email, NFC
	// password) so a differently-cased email or NFD input still authenticates.
	email = identity.NormalizeEmail(email)
	password = normalizePassword(password)

	// EVERY failure exit records one best-effort event — including exits forced
	// by a client disconnect (ctx cancellation during the lookup or the hash) or
	// an infra error, none of which change state. recordAudit detaches from the
	// request context, so the disconnect can't erase the trail (ADR-0009). The
	// event gains the actor once the user resolves; for an unknown email there is
	// no actor (and the attempted email stays out of the trail, to avoid
	// retaining junk input).
	failed := newEvent(ctx, audit.ActionAuthLogin, audit.OutcomeFailed)
	defer func() {
		if err != nil {
			s.recordAudit(ctx, failed)
		}
	}()

	// Oversized input can never match a stored credential (Bootstrap enforces the
	// same caps), so reject before any DB or hashing work — the upper password
	// bound exists precisely to cap Argon2 input cost on verification (ADR-0006).
	// The response stays the uniform credential rejection.
	if identity.EmailTooLong(email) || utf8.RuneCountInString(password) > maxPasswordLength {
		return Session{}, identity.ErrInvalidCredentials
	}

	// One query for the user + password hash + backoff state, so existing and
	// unknown accounts cost the same number of round-trips (anti-enumeration, OWASP).
	u, hash, backoff, err := s.repo.GetUserForLogin(ctx, email)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return Session{}, s.rejectWithEqualizedTiming(ctx, password)
		}
		return Session{}, err
	}
	failed = withActor(failed, u.ID)
	// Two rejections share one shape: a locked account (progressive backoff,
	// ADR-0006 — backoff.Locked is evaluated by the STORE on the database clock,
	// the same clock the failure upsert uses, so app/DB skew can't split the
	// decision) and an OIDC-only user (no password row). Both verify the dummy
	// hash so the timing matches a real attempt — while locked even a correct
	// guess is neither confirmed nor evaluated — and both count, so hammering a
	// locked account keeps doubling its window. One branch, so the twins can't
	// drift apart. An attempt aborted mid-equalizer by the caller counts
	// nothing, exactly like an aborted real verify below: nothing was evaluated.
	if backoff.Locked || hash == "" {
		rejErr := s.rejectWithEqualizedTiming(ctx, password)
		if ctx.Err() != nil {
			return Session{}, rejErr
		}
		s.noteLoginFailure(ctx, u.ID, &failed)
		return Session{}, rejErr
	}
	ok, needsRehash, err := s.hasher.Verify(ctx, password, hash)
	if err != nil {
		// Only caller cancellation propagates (the deferred failure event still
		// records the aborted attempt, and an aborted verify evaluated nothing, so
		// it does not count). Anything else — e.g. an unparsable stored hash —
		// gets the same generic rejection as a wrong password, so the response
		// stays uniform (ADR-0006); the cause is logged, types only.
		if ctx.Err() != nil {
			return Session{}, err
		}
		s.logger.WarnContext(ctx, "stored password hash unverifiable", "error_type", fmt.Sprintf("%T", err))
		s.noteLoginFailure(ctx, u.ID, &failed)
		return Session{}, identity.ErrInvalidCredentials
	}
	// Uniform result for wrong-password and disabled-account (OWASP: a generic error
	// regardless of whether the password was wrong or the account is disabled, with
	// equal timing). The disabled check is after the hash so it can't be probed
	// without the password, and returns the same error so it can't be probed with it.
	if !ok || !u.Active() {
		s.noteLoginFailure(ctx, u.ID, &failed)
		return Session{}, identity.ErrInvalidCredentials
	}
	if needsRehash {
		if nh, herr := s.hasher.Hash(ctx, password); herr == nil {
			_ = s.repo.SetPassword(ctx, u.ID, nh)
		}
	}
	// Success clears the backoff slate — best-effort (a missed reset self-heals
	// on the next success). Always issued so failures committed by concurrent
	// attempts DURING this verify are cleared too (a login-time snapshot check
	// would miss them); the statement's WHERE leaves an already-clean row
	// unwritten, so the hot path stays write-free.
	if rerr := s.repo.ResetLoginBackoff(ctx, u.ID); rerr != nil {
		s.logger.WarnContext(ctx, "login backoff reset failed", "error_type", fmt.Sprintf("%T", rerr))
	}
	return s.issueSession(ctx, u, nil, nil)
}

// noteLoginFailure counts a failed password attempt against a known account in
// ONE atomic statement that also imposes/extends the lockout once the
// threshold is crossed — window = min(base·2^(n-threshold), cap) spread by the
// jitter factor so the exact unlock time can't be probed (ADR-0006). One
// statement, one clock (the database's), so a concurrent success reset or lazy
// expiry-reset can never interleave between counting and locking. The write
// detaches from the request context — an attacker must not skip the counter by
// disconnecting mid-attempt — and is best-effort: a counter-store failure
// degrades the backoff, never the (already failing) login response. The
// failure audit event is tagged from the RETURNED state, so the trail only
// records lockouts that actually exist (ADR-0009) — no client-visible signal.
func (s *Service) noteLoginFailure(ctx context.Context, id identity.UserID, failed *audit.Event) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedWriteTimeout)
	defer cancel()
	b, err := s.repo.RecordLoginFailure(ctx, id, identity.FailureParams{
		Threshold: s.backoffThreshold,
		Base:      s.backoffBase,
		Cap:       s.backoffCap,
		// A counter idle past the cap is stale: whoever it belonged to has long
		// since been locked out or walked away, so it restarts rather than letting
		// months-old typos count toward a fresh lockout. Reusing the cap keeps the
		// scheme one-knob (pinned in ADR-0006).
		Staleness:    s.backoffCap,
		JitterFactor: 1 - backoffJitterFraction + 2*backoffJitterFraction*s.jitter(),
	})
	if err != nil {
		s.logger.WarnContext(ctx, "login backoff write failed", "error_type", fmt.Sprintf("%T", err))
		return
	}
	if b.Locked {
		failed.Metadata = map[string]any{"lockout": true, "backoff_failures": b.FailureCount}
	}
}

// PublicConfig reports the pre-session login surface: whether Google login is
// wired (WithOIDCProvider) and whether the instance still has zero users. The
// count is authoritative enough for routing — Bootstrap re-checks under a lock.
func (s *Service) PublicConfig(ctx context.Context) (PublicConfig, error) {
	n, err := s.repo.CountUsers(ctx)
	if err != nil {
		return PublicConfig{}, err
	}
	return PublicConfig{GoogleEnabled: s.oidc != nil, NeedsBootstrap: n == 0}, nil
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
	// The audit event commits with the revocation (one transaction, ADR-0009).
	return s.repo.RevokeSession(ctx, sess.ID,
		withActor(newEvent(ctx, audit.ActionAuthLogout, audit.OutcomeSucceeded), sess.UserID))
}

// Authenticate resolves the user for a raw session token, rejecting revoked,
// expired, or disabled sessions/users. It is a pure READ — the idle window is
// slid by SlideIdle, which the transport calls only AFTER the CSRF check passes,
// so a request that fails authorization can't keep a session alive.
func (s *Service) Authenticate(ctx context.Context, token string) (identity.User, identity.Session, error) {
	sum := sha256.Sum256([]byte(token))
	sess, err := s.repo.GetSessionByTokenHash(ctx, sum[:])
	if err != nil {
		return identity.User{}, identity.Session{}, err
	}
	if !sess.Valid(s.now()) {
		return identity.User{}, identity.Session{}, identity.ErrSessionNotFound
	}
	u, err := s.repo.GetUserByID(ctx, sess.UserID)
	if err != nil {
		return identity.User{}, identity.Session{}, err
	}
	if !u.Active() {
		return identity.User{}, identity.Session{}, identity.ErrUserDisabled
	}
	return u, sess, nil
}

// SlideIdle advances the session's idle expiry on activity, capped at the
// absolute expiry. The write is throttled to once per IdleRenewInterval so a busy
// session doesn't UPDATE on every request; the store uses greatest() so a
// late-arriving older request can't move the expiry backward. Even when the
// write is throttled, it performs a conditional validity read: logout, session
// rotation, and expiry must take effect before the handler runs on EVERY
// request, not merely on the next renewal write. Errors surface so the caller
// can fail the request.
func (s *Service) SlideIdle(ctx context.Context, sess identity.Session) error {
	newIdle := s.now().Add(s.idle)
	if newIdle.After(sess.AbsoluteExpiresAt) {
		newIdle = sess.AbsoluteExpiresAt
	}
	if newIdle.Sub(sess.IdleExpiresAt) < s.idleRenew {
		return s.repo.ValidateSession(ctx, sess.ID)
	}
	return s.repo.ExtendSessionIdle(ctx, sess.ID, newIdle)
}

// VerifyCSRF reports whether csrfToken is valid for the session identified by its
// raw token — the value in the __Host-portcullis_session cookie, which the
// transport interceptor holds (delegated to the protector; ADR-0006).
func (s *Service) VerifyCSRF(sessionToken, csrfToken string) bool {
	return s.csrf.Verify(sessionToken, csrfToken)
}

// rejectWithEqualizedTiming verifies the password against a dummy hash before
// returning ErrInvalidCredentials, so an unknown account costs the same hashing
// time as a wrong password (anti-enumeration, OWASP). Caller cancellation
// propagates (as it does on the known-user path), so a request aborted mid-verify
// returns the same context error for a known and an unknown email — otherwise the
// divergence (ctx error vs ErrInvalidCredentials) would itself be an enumeration
// signal. The deferred failure event still records the aborted attempt.
func (s *Service) rejectWithEqualizedTiming(ctx context.Context, password string) error {
	if _, _, err := s.hasher.Verify(ctx, password, s.dummyHash); err != nil && ctx.Err() != nil {
		return err
	}
	return identity.ErrInvalidCredentials
}

// issueSession mints and persists a session for a verified user. meta tags the
// success audit event (e.g. method=google); nil leaves it untagged, as password
// logins are.
func (s *Service) issueSession(ctx context.Context, u identity.User, meta map[string]any, link *identity.OIDCIdentity) (Session, error) {
	raw, err := newToken()
	if err != nil {
		return Session{}, err
	}
	// Issue the CSRF token (bound to the session token) before persisting, so the
	// rotation commit is the last fallible step — a CSRF failure can't leave the
	// user's prior sessions revoked with a new session they never received.
	csrf, err := s.csrf.Issue(raw)
	if err != nil {
		return Session{}, err
	}
	sum := sha256.Sum256([]byte(raw))
	sess := identity.NewSession("", u.ID, s.now(), s.idle, s.absolute)
	evt := withActor(newEvent(ctx, audit.ActionAuthLogin, audit.OutcomeSucceeded), u.ID)
	evt.Metadata = meta
	// One transaction: revoke the user's prior sessions, insert the new one, and
	// commit the login audit event with them — so concurrent logins still leave
	// exactly one active session (ADR-0006) and a successful login can never
	// commit without its trail (ADR-0009).
	var created identity.Session
	if link != nil {
		created, err = s.repo.LinkIdentityAndRotateSession(ctx, *link, sess, sum[:], evt)
	} else {
		created, err = s.repo.RotateSession(ctx, u.ID, sess, sum[:], evt)
	}
	if err != nil {
		return Session{}, err
	}
	return Session{User: u, Session: created, Token: raw, CSRF: csrf}, nil
}

// validateDisplayName bounds the stored display name and rejects control (Cc),
// format (Cf), and line/paragraph separator (Zl/Zp) characters, which could
// corrupt logs or the UI. unicode.IsControl catches only Cc, so each other class
// is tested explicitly: Cf covers bidi overrides (U+202E) and zero-width joiners
// that spoof rendered names; Zl/Zp (U+2028/U+2029) render as real line breaks that
// a plain "\n" — rejected as Cc — would, so omitting them leaves the same line
// injection open. Empty is allowed — the UI falls back to the email.
func validateDisplayName(name string) error {
	if utf8.RuneCountInString(name) > maxDisplayNameLength {
		return identity.ErrInvalidDisplayName
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return identity.ErrInvalidDisplayName
		}
	}
	return nil
}

// validatePassword enforces the length policy (see const.go) in Unicode code
// points, not bytes, so a short multi-byte password can't slip past. Length is
// the one requirement modern guidance agrees on; composition rules are
// discouraged. It expects an already NFC-normalized password.
func validatePassword(password string) error {
	n := utf8.RuneCountInString(password)
	if n < minPasswordLength || n > maxPasswordLength {
		return identity.ErrWeakPassword
	}
	return nil
}

// normalizePassword applies Unicode NFC so a password typed with equivalent code
// point sequences hashes and verifies identically across platforms/keyboards
// (NIST SP 800-63B). Applied before both hashing and verification.
func normalizePassword(password string) string {
	return norm.NFC.String(password)
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
