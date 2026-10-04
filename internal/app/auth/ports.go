package auth

import (
	"context"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Repository mutations commit audit events atomically and observe expiry under the request lock before deciding (ADR-0009/0018).
type Repository interface {
	// DefaultOrganizationID resolves the single self-hosted organization audit events are scoped to (single-org MVP).
	DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error)

	// Users & passwords.
	CountUsers(ctx context.Context) (int64, error)
	// GetUserForLogin returns the user, password hash, and progressive-backoff state in one query (the hash is "" for an OIDC-only user, the backoff is zero-valued when the account has never failed), so a password login costs the same number of round-trips whether or not the account exists.
	GetUserForLogin(ctx context.Context, email string) (identity.User, string, identity.LoginBackoff, error)
	GetUserByID(ctx context.Context, id identity.UserID) (identity.User, error)
	SetPassword(ctx context.Context, id identity.UserID, phc string) error

	// Backoff writes are atomic and use the database clock (ADR-0006). Expired or stale counters restart at one; active lockout expiry never moves backward.
	RecordLoginFailure(ctx context.Context, id identity.UserID, p identity.FailureParams) (identity.LoginBackoff, error)
	// ResetLoginBackoff clears the counter and lockout after a successful login; a row that is already clean is left unwritten (hot-path no-op).
	ResetLoginBackoff(ctx context.Context, id identity.UserID) error

	// BootstrapAdmin serializes first-admin creation and commits user, password, membership, and audit event together; partial failure leaves bootstrap retryable. A non-nil setupTokenHash must consume the outstanding unexpired setup token in the same transaction or the call fails with identity.ErrSetupTokenInvalid; nil marks operator provisioning from configuration (ADR-0052).
	BootstrapAdmin(ctx context.Context, email, displayName, passwordHash string, setupTokenHash []byte, evt audit.Event) (identity.User, error)
	// RotateSetupToken soft-revokes any outstanding setup token and stores the new hash with a database-clock expiry, serialized with BootstrapAdmin; it fails with identity.ErrAlreadyBootstrapped once a user exists (ADR-0052).
	RotateSetupToken(ctx context.Context, tokenHash []byte, ttl time.Duration) error

	// RotateSession atomically revokes the user's active sessions and inserts the new one in one transaction (ADR-0006), so a login leaves exactly one session. The audit event commits with the rotation (ADR-0009).
	RotateSession(ctx context.Context, user identity.UserID, s identity.Session, tokenHash []byte, evt audit.Event) (identity.Session, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (identity.Session, error)
	// RevokeSession revokes one session and commits the audit event with it (ADR-0009).
	RevokeSession(ctx context.Context, id identity.SessionID, evt audit.Event) error
	// ValidateSession confirms the session is still active against the server clock without extending its idle expiry. It closes the Authenticate→handler revocation/expiry race on requests that do not need an idle-slide write.
	ValidateSession(ctx context.Context, id identity.SessionID) error
	ExtendSessionIdle(ctx context.Context, id identity.SessionID, idle time.Time) error

	// External identities (OIDC login).
	FindUserBySubject(ctx context.Context, issuer, subject string) (identity.User, error)
	// LinkIdentityAndRotateSession atomically persists a first OIDC identity link, rotates the user's sessions, and commits the successful AUTH_LOGIN event. A linked authenticator must never survive without the session/audit outcome that authorized it (ADR-0007/0009).
	LinkIdentityAndRotateSession(ctx context.Context, id identity.OIDCIdentity, s identity.Session, tokenHash []byte, evt audit.Event) (identity.Session, error)
	// GetUserByEmail resolves a user by normalized email — the OIDC link-on-first- login lookup (ADR-0007: a verified provider email may attach to an existing admin-created account, never create one).
	GetUserByEmail(ctx context.Context, email string) (identity.User, error)
}

// OIDCProvider owns PKCE and ID-token signature, issuer, audience, and expiry checks. The service verifies the returned nonce (ADR-0007).
type OIDCProvider interface {
	AuthCodeURL(state, nonce, verifier string) string
	Exchange(ctx context.Context, code, verifier string) (identity.OIDCClaims, error)
}

// PasswordHasher permits cancellation while waiting for a bounded hashing slot; algorithms and parameters belong to the adapter.
type PasswordHasher interface {
	Hash(ctx context.Context, password string) (string, error)
	// Verify reports whether the password matches the encoded hash, and whether the hash should be re-computed because its parameters are out of date.
	Verify(ctx context.Context, password, encoded string) (ok, needsRehash bool, err error)
}

// AuditRecorder persists one audit event. The auth service records events best-effort: a failure is logged, never surfaced to the caller, so the audit trail cannot take authentication down with it. infra/postgres provides the append-only store adapter.
type AuditRecorder interface {
	Record(ctx context.Context, e audit.Event) error
}

// CSRFProtector binds tokens to the opaque session token before persistence; the adapter owns format and keying (ADR-0006).
type CSRFProtector interface {
	Issue(sessionToken string) (string, error)
	// Verify returns nil for a valid token, identity.ErrCSRFKeyVersionUnknown when the issuing key version is not loaded, and identity.ErrCSRFTokenInvalid otherwise.
	Verify(sessionToken, token string) error
}
