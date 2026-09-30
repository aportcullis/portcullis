package postgres

import (
	"context"
	"errors"
	"hash/fnv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// IdentityStore implements the identity domain's repository ports over the sqlc queries. A single type satisfies the user, role, OIDC, session, and permission catalog ports.
type IdentityStore struct {
	pool *pgxpool.Pool
	q    *db.Queries

	// orgID caches the single-org id (immutable after seeding) so audit writes on hot paths (every failed login, every rotate/revoke tx) don't re-query it.
	orgMu sync.Mutex
	orgID identity.OrganizationID
}

// NewIdentityStore builds the store on a connection pool.
func NewIdentityStore(pool *pgxpool.Pool) *IdentityStore {
	return &IdentityStore{pool: pool, q: db.New(pool)}
}

// userLockObject hashes a user id into the int32 object space for a per-user advisory lock; a collision only over-serializes two users' logins, which is harmless (the lock guards correctness, not exclusivity of access).
func userLockObject(user identity.UserID) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(user))
	return int32(h.Sum32())
}

// withLockedTx runs fn in a transaction holding the (class, object) advisory lock, committing on success and rolling back on error. It centralizes the tx + advisory-lock lifecycle so BootstrapAdmin and RotateSession can't drift apart.
func (s *IdentityStore) withLockedTx(ctx context.Context, class, object int32, fn func(*db.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful commit
	if _, err := tx.Exec(ctx, "select pg_advisory_xact_lock($1, $2)", class, object); err != nil {
		return err
	}
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func notFound(err, domainErr error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domainErr
	}
	return err
}

// onUniqueViolation maps SQLSTATE 23505 only when the optional constraint name matches; unrelated database errors pass through.
func onUniqueViolation(err error, constraint string, domainErr error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode &&
		(constraint == "" || pgErr.ConstraintName == constraint) {
		return domainErr
	}
	return err
}

func toUser(u db.User) identity.User {
	return identity.User{
		ID:          identity.UserID(uuidToString(u.ID)),
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Status:      identity.UserStatus(u.Status),
		CreatedAt:   tsToTime(u.CreatedAt),
	}
}

func toSession(s db.Session) identity.Session {
	return identity.Session{
		ID:                identity.SessionID(uuidToString(s.ID)),
		UserID:            identity.UserID(uuidToString(s.UserID)),
		IdleExpiresAt:     tsToTime(s.IdleExpiresAt),
		AbsoluteExpiresAt: tsToTime(s.AbsoluteExpiresAt),
		RevokedAt:         tsToTimePtr(s.RevokedAt),
		CreatedAt:         tsToTime(s.CreatedAt),
	}
}

// --- UserRepository ---

func (s *IdentityStore) CountUsers(ctx context.Context) (int64, error) {
	return s.q.CountUsers(ctx)
}

func (s *IdentityStore) DefaultOrganizationID(ctx context.Context) (identity.OrganizationID, error) {
	s.orgMu.Lock()
	cached := s.orgID
	s.orgMu.Unlock()
	if cached != "" {
		return cached, nil
	}
	org, err := s.q.GetDefaultOrganization(ctx)
	if err != nil {
		return "", err // don't cache a transient failure
	}
	id := identity.OrganizationID(uuidToString(org.ID))
	s.orgMu.Lock()
	s.orgID = id
	s.orgMu.Unlock()
	return id, nil
}

func (s *IdentityStore) CreateUser(ctx context.Context, email, displayName string) (identity.User, error) {
	u, err := s.q.CreateUser(ctx, db.CreateUserParams{Email: email, DisplayName: displayName})
	if err != nil {
		return identity.User{}, onUniqueViolation(err, usersEmailLowerIndex, identity.ErrEmailTaken)
	}
	return toUser(u), nil
}

func (s *IdentityStore) GetUserByEmail(ctx context.Context, email string) (identity.User, error) {
	u, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		return identity.User{}, notFound(err, identity.ErrUserNotFound)
	}
	return toUser(u), nil
}

// GetUserForLogin returns the user, password hash, and progressive-backoff state in one query; the hash is "" for an OIDC-only user with no password row, and the backoff is zero-valued for an account that never failed.
func (s *IdentityStore) GetUserForLogin(ctx context.Context, email string) (identity.User, string, identity.LoginBackoff, error) {
	row, err := s.q.GetUserForLogin(ctx, email)
	if err != nil {
		return identity.User{}, "", identity.LoginBackoff{}, notFound(err, identity.ErrUserNotFound)
	}
	u := identity.User{
		ID:          identity.UserID(uuidToString(row.ID)),
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Status:      identity.UserStatus(row.Status),
		CreatedAt:   tsToTime(row.CreatedAt),
	}
	backoff := identity.LoginBackoff{
		FailureCount: int(row.FailureCount),
		LockedUntil:  tsToTimePtr(row.LockedUntil),
		Locked:       row.Locked,
	}
	return u, row.PasswordHash, backoff, nil
}

// RecordLoginFailure counts one failed attempt and imposes/extends the lockout in a single atomic statement on the database clock (ADR-0006); the policy values travel per call, so the store stays policy-free.
func (s *IdentityStore) RecordLoginFailure(ctx context.Context, id identity.UserID, p identity.FailureParams) (identity.LoginBackoff, error) {
	uid, err := stringToUUID(string(id))
	if err != nil {
		return identity.LoginBackoff{}, identity.ErrUserNotFound
	}
	row, err := s.q.RecordLoginFailure(ctx, db.RecordLoginFailureParams{
		UserID:        uid,
		Threshold:     int32(p.Threshold),
		BaseSecs:      p.Base.Seconds(),
		CapSecs:       p.Cap.Seconds(),
		StalenessSecs: p.Staleness.Seconds(),
		JitterFactor:  p.JitterFactor,
	})
	if err != nil {
		return identity.LoginBackoff{}, err
	}
	return identity.LoginBackoff{
		FailureCount: int(row.FailureCount),
		LockedUntil:  tsToTimePtr(row.LockedUntil),
		Locked:       row.Locked,
	}, nil
}

// ResetLoginBackoff clears the counter and lockout after a successful login.
func (s *IdentityStore) ResetLoginBackoff(ctx context.Context, id identity.UserID) error {
	uid, err := stringToUUID(string(id))
	if err != nil {
		return identity.ErrUserNotFound
	}
	return s.q.ResetLoginBackoff(ctx, uid)
}

func (s *IdentityStore) GetUserByID(ctx context.Context, id identity.UserID) (identity.User, error) {
	uid, err := stringToUUID(string(id))
	if err != nil {
		return identity.User{}, identity.ErrUserNotFound
	}
	u, err := s.q.GetUserByID(ctx, uid)
	if err != nil {
		return identity.User{}, notFound(err, identity.ErrUserNotFound)
	}
	return toUser(u), nil
}

func (s *IdentityStore) SetPassword(ctx context.Context, id identity.UserID, phc string) error {
	uid, err := stringToUUID(string(id))
	if err != nil {
		return err
	}
	return s.q.UpsertPasswordAuth(ctx, db.UpsertPasswordAuthParams{UserID: uid, Secret: phc})
}

func (s *IdentityStore) GetPasswordHash(ctx context.Context, id identity.UserID) (string, error) {
	uid, err := stringToUUID(string(id))
	if err != nil {
		return "", err
	}
	am, err := s.q.GetPasswordAuth(ctx, uid)
	if err != nil {
		return "", notFound(err, identity.ErrUserNotFound)
	}
	return am.Secret, nil
}

func (s *IdentityStore) AddMembership(ctx context.Context, org identity.OrganizationID, user identity.UserID, role identity.RoleID) error {
	orgU, err := stringToUUID(string(org))
	if err != nil {
		return err
	}
	userU, err := stringToUUID(string(user))
	if err != nil {
		return err
	}
	roleU, err := stringToUUID(string(role))
	if err != nil {
		return err
	}
	_, err = s.q.CreateMembership(ctx, db.CreateMembershipParams{OrganizationID: orgU, UserID: userU, RoleID: roleU})
	// The one unique constraint on the insert is (organization_id, user_id); map its violation to the domain sentinel rather than leaking the raw driver error.
	return onUniqueViolation(err, "", identity.ErrMembershipExists)
}

// RoleNameForUser returns the display name of the user's role in the organization — a UI label only, never an authorization input (ADR-0008). A user without an active membership/role maps to ErrUserNotFound.
func (s *IdentityStore) RoleNameForUser(ctx context.Context, org identity.OrganizationID, id identity.UserID) (string, error) {
	orgU, err := stringToUUID(string(org))
	if err != nil {
		return "", err
	}
	uid, err := stringToUUID(string(id))
	if err != nil {
		return "", err
	}
	name, err := s.q.RoleNameForUser(ctx, db.RoleNameForUserParams{OrganizationID: orgU, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", identity.ErrUserNotFound
	}
	if err != nil {
		return "", err
	}
	return name, nil
}

func (s *IdentityStore) PermissionsForUser(ctx context.Context, org identity.OrganizationID, id identity.UserID) ([]identity.Permission, error) {
	orgU, err := stringToUUID(string(org))
	if err != nil {
		return nil, err
	}
	uid, err := stringToUUID(string(id))
	if err != nil {
		return nil, err
	}
	keys, err := s.q.PermissionsForUser(ctx, db.PermissionsForUserParams{OrganizationID: orgU, UserID: uid})
	if err != nil {
		return nil, err
	}
	perms := make([]identity.Permission, 0, len(keys))
	for _, k := range keys {
		perms = append(perms, identity.Permission(k))
	}
	return perms, nil
}

// BootstrapAdmin serializes first-admin creation and commits user, password, membership, and audit event together; partial failure leaves bootstrap retryable.
func (s *IdentityStore) BootstrapAdmin(ctx context.Context, email, displayName, passwordHash string, evt audit.Event) (identity.User, error) {
	var out identity.User
	err := s.withLockedTx(ctx, lockClassBootstrap, 0, func(q *db.Queries) error {
		n, err := q.CountUsers(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return identity.ErrAlreadyBootstrapped
		}
		org, err := q.GetDefaultOrganization(ctx)
		if err != nil {
			return err
		}
		role, err := q.BootstrapRoleID(ctx, org.ID)
		if err != nil {
			return notFound(err, errors.New("postgres: no bootstrap-default role seeded"))
		}
		u, err := q.CreateUser(ctx, db.CreateUserParams{Email: email, DisplayName: displayName})
		if err != nil {
			return err
		}
		if err := q.UpsertPasswordAuth(ctx, db.UpsertPasswordAuthParams{UserID: u.ID, Secret: passwordHash}); err != nil {
			return err
		}
		if _, err := q.CreateMembership(ctx, db.CreateMembershipParams{OrganizationID: org.ID, UserID: u.ID, RoleID: role}); err != nil {
			return err
		}
		out = toUser(u)
		evt.OrganizationID = identity.OrganizationID(uuidToString(org.ID))
		evt.ActorUserID = &out.ID
		evt.TargetType = audit.TargetTypeUser
		evt.TargetID = string(out.ID)
		return insertAuditTx(ctx, q, evt)
	})
	return out, err
}

// --- RoleRepository ---

func (s *IdentityStore) BootstrapRoleID(ctx context.Context, org identity.OrganizationID) (identity.RoleID, error) {
	orgU, err := stringToUUID(string(org))
	if err != nil {
		return "", err
	}
	rid, err := s.q.BootstrapRoleID(ctx, orgU)
	if err != nil {
		return "", notFound(err, errors.New("postgres: no bootstrap-default role seeded"))
	}
	return identity.RoleID(uuidToString(rid)), nil
}

// --- PermissionCatalog ---

func (s *IdentityStore) ListPermissions(ctx context.Context) ([]identity.Permission, error) {
	keys, err := s.q.ListPermissionKeys(ctx)
	if err != nil {
		return nil, err
	}
	perms := make([]identity.Permission, 0, len(keys))
	for _, k := range keys {
		perms = append(perms, identity.Permission(k))
	}
	return perms, nil
}

// --- OIDCRepository ---

func (s *IdentityStore) FindUserBySubject(ctx context.Context, issuer, subject string) (identity.User, error) {
	u, err := s.q.FindUserBySubject(ctx, db.FindUserBySubjectParams{Issuer: issuer, Subject: subject})
	if err != nil {
		return identity.User{}, notFound(err, identity.ErrNoLinkedAccount)
	}
	return toUser(u), nil
}

func linkOIDCIdentity(ctx context.Context, q *db.Queries, id identity.OIDCIdentity) error {
	uid, err := stringToUUID(string(id.UserID))
	if err != nil {
		return err
	}
	// No row comes back when the (issuer, subject) is already linked to a different user (the conflict's WHERE fails), which we surface as a collision.
	_, err = q.LinkOIDCIdentity(ctx, db.LinkOIDCIdentityParams{
		UserID:  uid,
		Issuer:  id.Issuer,
		Subject: id.Subject,
		Email:   id.Email,
	})
	return notFound(err, identity.ErrIdentityLinkedToAnotherUser)
}

// --- SessionRepository ---

func (s *IdentityStore) CreateSession(ctx context.Context, sess identity.Session, tokenHash []byte) (identity.Session, error) {
	uid, err := stringToUUID(string(sess.UserID))
	if err != nil {
		return identity.Session{}, err
	}
	row, err := s.q.CreateSession(ctx, db.CreateSessionParams{
		UserID:            uid,
		TokenHash:         tokenHash,
		IdleExpiresAt:     timeToTS(sess.IdleExpiresAt),
		AbsoluteExpiresAt: timeToTS(sess.AbsoluteExpiresAt),
	})
	if err != nil {
		return identity.Session{}, err
	}
	return toSession(row), nil
}

func (s *IdentityStore) GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (identity.Session, error) {
	row, err := s.q.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return identity.Session{}, notFound(err, identity.ErrSessionNotFound)
	}
	return toSession(row), nil
}

// ValidateSession is the no-write counterpart to ExtendSessionIdle. It is run after CSRF succeeds when a throttled request does not need to renew idle expiry, making server-side revocation and expiry effective on every request.
func (s *IdentityStore) ValidateSession(ctx context.Context, id identity.SessionID) error {
	sid, err := stringToUUID(string(id))
	if err != nil {
		return identity.ErrSessionNotFound
	}
	if _, err := s.q.ValidateSession(ctx, sid); err != nil {
		return notFound(err, identity.ErrSessionNotFound)
	}
	return nil
}

// completeEventOrg fills a missing OrganizationID from the cached single-org id BEFORE the transaction opens (insertAuditTx can also resolve it, but only via an extra query inside the tx). Every state-changing method that rides an audit event calls this one helper so the invariant can't be forgotten per call site.
func (s *IdentityStore) completeEventOrg(ctx context.Context, evt *audit.Event) error {
	if evt.OrganizationID != "" {
		return nil
	}
	org, err := s.DefaultOrganizationID(ctx)
	if err != nil {
		return err
	}
	evt.OrganizationID = org
	return nil
}

// RevokeSession revokes one session and writes the audit event in the same transaction (ADR-0009), so a logout can never commit without its trail.
func (s *IdentityStore) RevokeSession(ctx context.Context, id identity.SessionID, evt audit.Event) error {
	sid, err := stringToUUID(string(id))
	if err != nil {
		return err
	}
	if err := s.completeEventOrg(ctx, &evt); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful commit
	q := s.q.WithTx(tx)
	rows, err := q.RevokeSession(ctx, sid)
	if err != nil {
		return err
	}
	if rows == 0 {
		// Already revoked (concurrent double logout): no state changed, so no audit event — the trail must mirror real state changes (ADR-0009) and the original revoked_at stays intact. Nothing to commit.
		return nil
	}
	if err := insertAuditTx(ctx, q, evt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *IdentityStore) RevokeUserSessions(ctx context.Context, user identity.UserID) error {
	uid, err := stringToUUID(string(user))
	if err != nil {
		return err
	}
	return s.q.RevokeUserSessions(ctx, uid)
}

func (s *IdentityStore) ExtendSessionIdle(ctx context.Context, id identity.SessionID, idle time.Time) error {
	sid, err := stringToUUID(string(id))
	if err != nil {
		return err
	}
	updated, err := s.q.ExtendSessionIdle(ctx, db.ExtendSessionIdleParams{ID: sid, IdleExpiresAt: timeToTS(idle)})
	if err != nil {
		return err
	}
	if updated == 0 {
		// Authenticate and the post-CSRF slide are deliberately separate so a rejected request cannot prolong a session. The conditional UPDATE is therefore the authoritative final check: a concurrent revocation or expiry must reject this request, not merely avoid resurrection.
		return identity.ErrSessionNotFound
	}
	return nil
}

// RotateSession revokes the user's active sessions, inserts the new one, and writes the login audit event in one transaction, serialized by a per-user advisory lock — concurrent logins still leave exactly one active session (ADR-0006) and a session can never be issued without its trail (ADR-0009).
func (s *IdentityStore) RotateSession(ctx context.Context, user identity.UserID, sess identity.Session, tokenHash []byte, evt audit.Event) (identity.Session, error) {
	uid, err := stringToUUID(string(user))
	if err != nil {
		return identity.Session{}, err
	}
	if err := s.completeEventOrg(ctx, &evt); err != nil {
		return identity.Session{}, err
	}
	var out identity.Session
	err = s.withLockedTx(ctx, lockClassSession, userLockObject(user), func(q *db.Queries) error {
		return rotateSessionTx(ctx, q, uid, sess, tokenHash, evt, &out)
	})
	return out, err
}

// LinkIdentityAndRotateSession makes first OIDC login one atomic security state change: the provider subject link, session rotation, and AUTH_LOGIN evidence either all commit or all roll back (ADR-0007/0009).
func (s *IdentityStore) LinkIdentityAndRotateSession(ctx context.Context, link identity.OIDCIdentity, sess identity.Session, tokenHash []byte, evt audit.Event) (identity.Session, error) {
	uid, err := stringToUUID(string(link.UserID))
	if err != nil {
		return identity.Session{}, err
	}
	if err := s.completeEventOrg(ctx, &evt); err != nil {
		return identity.Session{}, err
	}
	var out identity.Session
	err = s.withLockedTx(ctx, lockClassSession, userLockObject(link.UserID), func(q *db.Queries) error {
		if err := linkOIDCIdentity(ctx, q, link); err != nil {
			return err
		}
		return rotateSessionTx(ctx, q, uid, sess, tokenHash, evt, &out)
	})
	return out, err
}

func rotateSessionTx(ctx context.Context, q *db.Queries, user pgtype.UUID, sess identity.Session, tokenHash []byte, evt audit.Event, out *identity.Session) error {
	if err := q.RevokeUserSessions(ctx, user); err != nil {
		return err
	}
	row, err := q.CreateSession(ctx, db.CreateSessionParams{
		UserID:            user,
		TokenHash:         tokenHash,
		IdleExpiresAt:     timeToTS(sess.IdleExpiresAt),
		AbsoluteExpiresAt: timeToTS(sess.AbsoluteExpiresAt),
	})
	if err != nil {
		return err
	}
	*out = toSession(row)
	return insertAuditTx(ctx, q, evt)
}
