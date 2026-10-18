package postgres

import (
	"context"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// passwordSetupStorageTimeout bounds digest resolution and atomic consumption, including lock waits.
const passwordSetupStorageTimeout = 5 * time.Second

// ResolvePasswordSetup resolves only a live initial-password link for the configured organization.
func (s *IdentityStore) ResolvePasswordSetup(ctx context.Context, digest []byte) (identity.PasswordSetup, error) {
	ctx, cancel := context.WithTimeout(ctx, passwordSetupStorageTimeout)
	defer cancel()
	if len(digest) != 32 {
		return identity.PasswordSetup{}, identity.ErrPasswordSetupInvalid
	}
	organization, err := s.DefaultOrganizationID(ctx)
	if err != nil {
		return identity.PasswordSetup{}, err
	}
	organizationID, err := policyOrganizationUUID(organization)
	if err != nil {
		return identity.PasswordSetup{}, err
	}
	row, err := s.q.ResolvePasswordSetup(ctx, db.ResolvePasswordSetupParams{TokenHash: digest, OrganizationID: organizationID})
	if err != nil {
		return identity.PasswordSetup{}, notFound(err, identity.ErrPasswordSetupInvalid)
	}
	return identity.PasswordSetup{UserID: identity.UserID(uuidToString(row.UserID)), OrganizationID: organization, ExpiresAt: tsToTime(row.ExpiresAt)}, nil
}

// CompletePasswordSetup serializes initial credentials with policy and session mutations.
func (s *IdentityStore) CompletePasswordSetup(ctx context.Context, digest []byte, passwordHash string, event audit.Event) error {
	ctx, cancel := context.WithTimeout(ctx, passwordSetupStorageTimeout)
	defer cancel()
	hint, err := s.ResolvePasswordSetup(ctx, digest)
	if err != nil {
		return err
	}
	organizationID, err := policyOrganizationUUID(hint.OrganizationID)
	if err != nil {
		return err
	}
	userID, valid := parseBindingPolicyUUID(string(hint.UserID))
	if !valid {
		return identity.ErrPasswordSetupInvalid
	}
	return s.withLockedTx(ctx, lockClassAdministration, organizationLockObject(hint.OrganizationID), func(queries *db.Queries) error {
		account, err := queries.LockPolicyAccount(ctx, db.LockPolicyAccountParams{OrganizationID: organizationID, UserID: userID})
		if err != nil {
			return notFound(err, identity.ErrPasswordSetupInvalid)
		}
		if account.Status != string(identity.StatusActive) || account.HasPassword {
			return identity.ErrPasswordSetupInvalid
		}
		if err := queries.LockBindingSubjectSession(ctx, db.LockBindingSubjectSessionParams{LockClass: lockClassSession, LockObject: userLockObject(hint.UserID)}); err != nil {
			return err
		}
		at, err := observeInstant(ctx, queries)
		if err != nil {
			return err
		}
		count, err := queries.ConsumePasswordSetup(ctx, db.ConsumePasswordSetupParams{At: timeToTS(at), TokenHash: digest, OrganizationID: organizationID, UserID: userID})
		if err != nil {
			return err
		}
		if count != 1 {
			return identity.ErrPasswordSetupInvalid
		}
		count, err = queries.InsertInitialPassword(ctx, db.InsertInitialPasswordParams{UserID: userID, Secret: passwordHash, At: timeToTS(at)})
		if err != nil {
			return err
		}
		if count != 1 {
			return identity.ErrPasswordSetupInvalid
		}
		revoked, err := queries.RevokePasswordSetupSessions(ctx, db.RevokePasswordSetupSessionsParams{At: timeToTS(at), UserID: userID})
		if err != nil {
			return err
		}
		event.OrganizationID = hint.OrganizationID
		event.ActorUserID = &hint.UserID
		event.ActorType = audit.ActorUser
		event.TargetType = audit.TargetTypeUser
		event.TargetID = string(hint.UserID)
		event.OccurredAt = at
		event.Metadata = map[string]any{"revoked_sessions": revoked}
		return insertAuditTx(ctx, queries, event)
	})
}
