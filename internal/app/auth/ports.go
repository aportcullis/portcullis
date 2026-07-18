package auth

import (
	"context"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Repository is the narrow storage surface the auth use cases need — only the
// methods this service calls (ISP), not the full domain repositories. The
// postgres IdentityStore satisfies it, and tests use an in-memory fake.
type Repository interface {
	// DefaultOrganizationID resolves the single self-hosted organization audit
	// events are scoped to (single-org MVP).
	DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error)

	// Users & passwords.
	CountUsers(ctx context.Context) (int64, error)
	// GetUserForLogin returns the user, password hash, and progressive-backoff
	// state in one query (the hash is "" for an OIDC-only user, the backoff is
	// zero-valued when the account has never failed), so a password login costs
	// the same number of round-trips whether or not the account exists.
	GetUserForLogin(ctx context.Context, email string) (identity.User, string, identity.LoginBackoff, error)
	GetUserByID(ctx context.Context, id identity.UserID) (identity.User, error)
	SetPassword(ctx context.Context, id identity.UserID, phc string) error

	// Progressive backoff (ADR-0006). Both are atomic single-statement writes
	// evaluated on the DATABASE clock, so concurrent logins across replicas
	// can't lose updates and app/DB clock skew can't split the expiry decision.
	//
	// RecordLoginFailure counts one failed password attempt under the given
	// policy and imposes/extends the lockout once the threshold is crossed
	// (never moving an existing expiry backward), returning the resulting
	// state. A counter whose lockout has expired — or whose last failure is
	// older than the staleness window — restarts at 1.
	RecordLoginFailure(ctx context.Context, id identity.UserID, p identity.FailureParams) (identity.LoginBackoff, error)
	// ResetLoginBackoff clears the counter and lockout after a successful
	// login; a row that is already clean is left unwritten (hot-path no-op).
	ResetLoginBackoff(ctx context.Context, id identity.UserID) error

	// BootstrapAdmin atomically creates the first admin (user + password +
	// bootstrap-role membership) under a lock, returning ErrAlreadyBootstrapped if
	// any user already exists. The password is pre-hashed by the caller. The audit
	// event is written in the SAME transaction (ADR-0009) — the store completes
	// its organization and actor (the created user exists only inside the tx).
	BootstrapAdmin(ctx context.Context, email, displayName, passwordHash string, evt audit.Event) (identity.User, error)

	// RotateSession atomically revokes the user's active sessions and inserts the
	// new one in one transaction (ADR-0006), so a login leaves exactly one session.
	// The audit event commits with the rotation (ADR-0009).
	RotateSession(ctx context.Context, user identity.UserID, s identity.Session, tokenHash []byte, evt audit.Event) (identity.Session, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (identity.Session, error)
	// RevokeSession revokes one session and commits the audit event with it
	// (ADR-0009).
	RevokeSession(ctx context.Context, id identity.SessionID, evt audit.Event) error
	// ValidateSession confirms the session is still active against the server
	// clock without extending its idle expiry. It closes the Authenticate→handler
	// revocation/expiry race on requests that do not need an idle-slide write.
	ValidateSession(ctx context.Context, id identity.SessionID) error
	ExtendSessionIdle(ctx context.Context, id identity.SessionID, idle time.Time) error

	// External identities (OIDC login).
	FindUserBySubject(ctx context.Context, issuer, subject string) (identity.User, error)
	// LinkIdentityAndRotateSession atomically persists a first OIDC identity link,
	// rotates the user's sessions, and commits the successful AUTH_LOGIN event.
	// A linked authenticator must never survive without the session/audit outcome
	// that authorized it (ADR-0007/0009).
	LinkIdentityAndRotateSession(ctx context.Context, id identity.OIDCIdentity, s identity.Session, tokenHash []byte, evt audit.Event) (identity.Session, error)
	// GetUserByEmail resolves a user by normalized email — the OIDC link-on-first-
	// login lookup (ADR-0007: a verified provider email may attach to an existing
	// admin-created account, never create one).
	GetUserByEmail(ctx context.Context, email string) (identity.User, error)
}

// OIDCProvider is the use case's view of an external OIDC provider (Google —
// ADR-0007). The adapter owns the OAuth2/OIDC protocol detail: AuthCodeURL
// derives the S256 challenge from the verifier and requests exactly the
// openid/email/profile scopes; Exchange redeems the code with the PKCE verifier
// and verifies the ID token's signature, issuer, audience, and expiry. The
// returned claims carry the token's nonce UNVERIFIED — the service compares it
// against the pending value, so that check stays scenario-testable.
type OIDCProvider interface {
	AuthCodeURL(state, nonce, verifier string) string
	Exchange(ctx context.Context, code, verifier string) (identity.OIDCClaims, error)
}

// PasswordHasher hashes and verifies passwords. The application depends on this
// small, consumer-defined interface (ISP/DIP) rather than the concrete crypto
// package, so the algorithm and its parameters stay an injected detail and tests
// can substitute a fast fake. infra/crypto provides the production adapter.
// A ctx is passed so the bounded hasher can abandon a request that is cancelled
// while waiting for a concurrency slot.
type PasswordHasher interface {
	Hash(ctx context.Context, password string) (string, error)
	// Verify reports whether the password matches the encoded hash, and whether
	// the hash should be re-computed because its parameters are out of date.
	Verify(ctx context.Context, password, encoded string) (ok, needsRehash bool, err error)
}

// AuditRecorder persists one audit event. The auth service records events
// best-effort: a failure is logged, never surfaced to the caller, so the audit
// trail cannot take authentication down with it. infra/postgres provides the
// append-only store adapter.
type AuditRecorder interface {
	Record(ctx context.Context, e audit.Event) error
}

// CSRFProtector issues and verifies CSRF tokens bound to the session token
// (ADR-0006: HMAC over session_token || nonce, nonce suffixed). It is a use-case
// port — the token format, nonce, and keying are the adapter's concern, not the
// service's. The token is issued before the session is persisted, so the binding
// value is the opaque session token, not the DB id; infra/crypto provides the
// keyring-backed adapter, and a plain string keeps it free of the identity domain.
type CSRFProtector interface {
	Issue(sessionToken string) (string, error)
	Verify(sessionToken, token string) bool
}
