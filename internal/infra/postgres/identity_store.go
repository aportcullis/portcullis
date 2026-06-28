package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// bootstrapAdvisoryLock serializes concurrent first-run bootstraps. The value is
// arbitrary but fixed ('PORT'); the lock is held for the bootstrap transaction
// and released when it ends.
const bootstrapAdvisoryLock int64 = 0x504F5254

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

func (s *IdentityStore) PermissionsForUser(ctx context.Context, id identity.UserID) ([]identity.Permission, error) {
	uid, err := stringToUUID(string(id))
	if err != nil {
		return nil, err
	}
	keys, err := s.q.PermissionsForUser(ctx, uid)
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
// the user, password, and bootstrap-role membership in one transaction. A partial
// failure rolls back fully, so a retry can still bootstrap.
func (s *IdentityStore) BootstrapAdmin(ctx context.Context, email, displayName, passwordHash string) (identity.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.User{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful commit
	q := s.q.WithTx(tx)

	if _, err := tx.Exec(ctx, "select pg_advisory_xact_lock($1)", bootstrapAdvisoryLock); err != nil {
		return identity.User{}, err
	}
	n, err := q.CountUsers(ctx)
	if err != nil {
		return identity.User{}, err
	}
	if n > 0 {
		return identity.User{}, identity.ErrAlreadyBootstrapped
	}

	org, err := q.GetDefaultOrganization(ctx)
	if err != nil {
		return identity.User{}, err
	}
	role, err := q.BootstrapRoleID(ctx, org.ID)
	if err != nil {
		return identity.User{}, notFound(err, errors.New("postgres: no bootstrap-default role seeded"))
	}
	u, err := q.CreateUser(ctx, db.CreateUserParams{Email: email, DisplayName: displayName})
	if err != nil {
		return identity.User{}, err
	}
	if err := q.UpsertPasswordAuth(ctx, db.UpsertPasswordAuthParams{UserID: u.ID, Secret: passwordHash}); err != nil {
		return identity.User{}, err
	}
	if _, err := q.CreateMembership(ctx, db.CreateMembershipParams{OrganizationID: org.ID, UserID: u.ID, RoleID: role}); err != nil {
		return identity.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.User{}, err
	}
	return toUser(u), nil
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

func (s *IdentityStore) RevokeSession(ctx context.Context, id identity.SessionID) error {
	sid, err := stringToUUID(string(id))
	if err != nil {
		return err
	}
	return s.q.RevokeSession(ctx, sid)
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
