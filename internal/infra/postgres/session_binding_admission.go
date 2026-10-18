package postgres

import (
	"context"
	"errors"

	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/jackc/pgx/v5"
)

// lockCurrentSessionMember observes independent account participation before session rotation under the organization lock.
func lockCurrentSessionMember(ctx context.Context, queries *db.Queries, organization identity.OrganizationID, user identity.UserID) error {
	organizationID, valid := parseBindingPolicyUUID(string(organization))
	if !valid {
		return identity.ErrSessionNotFound
	}
	userID, valid := parseBindingPolicyUUID(string(user))
	if !valid {
		return identity.ErrSessionNotFound
	}
	status, err := queries.LockBindingMember(ctx, db.LockBindingMemberParams{OrganizationID: organizationID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrSessionNotFound
	}
	if err != nil {
		return err
	}
	if status != string(identity.StatusActive) {
		return identity.ErrUserDisabled
	}
	return queries.LockBindingSubjectSession(ctx, db.LockBindingSubjectSessionParams{LockClass: lockClassSession, LockObject: userLockObject(user)})
}
