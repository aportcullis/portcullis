package postgres

import (
	"context"
	"errors"
	"hash/fnv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// IdentityStore implements the identity domain's repository ports over the sqlc
// queries. A single type satisfies the user, role, OIDC, session, and permission
// catalog ports.
type IdentityStore struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// NewIdentityStore builds the store on a connection pool.
func NewIdentityStore(pool *pgxpool.Pool) *IdentityStore {
	return &IdentityStore{pool: pool, q: db.New(pool)}
}

// userLockObject hashes a user id into the int32 object space for a per-user
// advisory lock; a collision only over-serializes two users' logins, which is
// harmless (the lock guards correctness, not exclusivity of access).
func userLockObject(user identity.UserID) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(user))
	return int32(h.Sum32())
}

// withLockedTx runs fn in a transaction holding the (class, object) advisory lock,
// committing on success and rolling back on error. It centralizes the tx +
// advisory-lock lifecycle so BootstrapAdmin and RotateSession can't drift apart.
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
	org, err := s.q.GetDefaultOrganization(ctx)
	if err != nil {
		return "", err
	}
	return identity.OrganizationID(uuidToString(org.ID)), nil
}

func (s *IdentityStore) CreateUser(ctx context.Context, email, displayName string) (identity.User, error) {
	u, err := s.q.CreateUser(ctx, db.CreateUserParams{Email: email, DisplayName: displayName})
	if err != nil {
		return identity.User{}, err
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

// GetUserForLogin returns the user and password hash in one query; the hash is
// "" for an OIDC-only user with no password row.
func (s *IdentityStore) GetUserForLogin(ctx context.Context, email string) (identity.User, string, error) {
	row, err := s.q.GetUserForLogin(ctx, email)
	if err != nil {
		return identity.User{}, "", notFound(err, identity.ErrUserNotFound)
	}
	u := identity.User{
		ID:          identity.UserID(uuidToString(row.ID)),
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Status:      identity.UserStatus(row.Status),
		CreatedAt:   tsToTime(row.CreatedAt),
	}
	return u, row.PasswordHash, nil
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
	return err
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

// BootstrapAdmin atomically creates the first admin: under an advisory lock (so
// concurrent bootstraps serialize), it re-checks that no user exists, then writes
// the user, password, bootstrap-role membership, AND the audit event in one
// transaction (ADR-0009) — completing the event's actor with the user created
// inside the tx. A partial failure rolls back fully, so a retry can still
// bootstrap and a created admin can never lack its trail.
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

func (s *IdentityStore) LinkIdentity(ctx context.Context, id identity.OIDCIdentity) error {
	uid, err := stringToUUID(string(id.UserID))
	if err != nil {
		return err
	}
	// No row comes back when the (issuer, subject) is already linked to a
	// different user (the conflict's WHERE fails), which we surface as a collision.
	_, err = s.q.LinkOIDCIdentity(ctx, db.LinkOIDCIdentityParams{
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

// RevokeSession revokes one session and writes the audit event in the same
// transaction (ADR-0009), so a logout can never commit without its trail.
func (s *IdentityStore) RevokeSession(ctx context.Context, id identity.SessionID, evt audit.Event) error {
	sid, err := stringToUUID(string(id))
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful commit
	q := s.q.WithTx(tx)
	if err := q.RevokeSession(ctx, sid); err != nil {
		return err
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
	return s.q.ExtendSessionIdle(ctx, db.ExtendSessionIdleParams{ID: sid, IdleExpiresAt: timeToTS(idle)})
}

// RotateSession revokes the user's active sessions, inserts the new one, and
// writes the login audit event in one transaction, serialized by a per-user
// advisory lock — concurrent logins still leave exactly one active session
// (ADR-0006) and a session can never be issued without its trail (ADR-0009).
func (s *IdentityStore) RotateSession(ctx context.Context, user identity.UserID, sess identity.Session, tokenHash []byte, evt audit.Event) (identity.Session, error) {
	uid, err := stringToUUID(string(user))
	if err != nil {
		return identity.Session{}, err
	}
	var out identity.Session
	err = s.withLockedTx(ctx, lockClassSession, userLockObject(user), func(q *db.Queries) error {
		if err := q.RevokeUserSessions(ctx, uid); err != nil {
			return err
		}
		row, err := q.CreateSession(ctx, db.CreateSessionParams{
			UserID:            uid,
			TokenHash:         tokenHash,
			IdleExpiresAt:     timeToTS(sess.IdleExpiresAt),
			AbsoluteExpiresAt: timeToTS(sess.AbsoluteExpiresAt),
		})
		if err != nil {
			return err
		}
		out = toSession(row)
		return insertAuditTx(ctx, q, evt)
	})
	return out, err
}
