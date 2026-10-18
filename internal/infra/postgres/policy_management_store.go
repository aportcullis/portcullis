package postgres

import (
	"context"
	"slices"
	"time"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// WithAccountPolicyTransaction supplies an independent membership mutation boundary.
func (s *BindingManagementStore) WithAccountPolicyTransaction(ctx context.Context, organization identity.OrganizationID, mutate func(administration.AccountPolicyTransaction) error) error {
	return s.withPolicyTransaction(ctx, organization, func(transaction *bindingManagementTransaction) error { return mutate(transaction) })
}

// WithRolePolicyTransaction supplies a shared role mutation boundary.
func (s *BindingManagementStore) WithRolePolicyTransaction(ctx context.Context, organization identity.OrganizationID, mutate func(administration.RolePolicyTransaction) error) error {
	return s.withPolicyTransaction(ctx, organization, func(transaction *bindingManagementTransaction) error { return mutate(transaction) })
}

// ObservePolicyMutationTime locks all affected subjects in canonical order before reading database wall time.
func (transaction *bindingManagementTransaction) ObservePolicyMutationTime(ctx context.Context, subjects []identity.UserID) (time.Time, error) {
	ordered := slices.Clone(subjects)
	slices.Sort(ordered)
	ordered = slices.Compact(ordered)
	for _, user := range ordered {
		userID, valid := parseBindingPolicyUUID(string(user))
		if !valid {
			return time.Time{}, identity.ErrUserNotFound
		}
		if err := transaction.queries.LockBindingSubjectSession(ctx, db.LockBindingSubjectSessionParams{LockClass: lockClassSession, LockObject: userLockObject(user)}); err != nil {
			return time.Time{}, err
		}
		if err := transaction.queries.LockBindingSessionRows(ctx, userID); err != nil {
			return time.Time{}, err
		}
	}
	at, err := observeInstant(ctx, transaction.queries)
	transaction.at = at
	return at, err
}

// FinalizePolicyChange revokes every affected subject and advances revision exactly once with audit evidence.
func (transaction *bindingManagementTransaction) FinalizePolicyChange(ctx context.Context, subjects []identity.UserID, event audit.Event) (int64, error) {
	if transaction.at.IsZero() || !event.OccurredAt.Equal(transaction.at) || event.OrganizationID != transaction.organization {
		return 0, administration.ErrBindingManagementUnavailable
	}
	ordered := slices.Clone(subjects)
	slices.Sort(ordered)
	ordered = slices.Compact(ordered)
	var revoked int64
	for _, user := range ordered {
		userID, valid := parseBindingPolicyUUID(string(user))
		if !valid {
			return 0, identity.ErrUserNotFound
		}
		count, err := transaction.queries.RevokeUserSessionsAt(ctx, db.RevokeUserSessionsAtParams{UserID: userID, At: timeToTS(transaction.at)})
		if err != nil {
			return 0, err
		}
		revoked += count
	}
	revision, err := transaction.queries.AdvanceBindingPolicyRevision(ctx, db.AdvanceBindingPolicyRevisionParams{OrganizationID: transaction.organizationID, ExpectedRevision: transaction.revision})
	if err != nil {
		return 0, err
	}
	if event.Metadata == nil {
		event.Metadata = make(map[string]any)
	}
	event.Metadata["authorization_revision"] = revision
	event.Metadata["revoked_sessions"] = revoked
	if err := insertAuditTx(ctx, transaction.queries, event); err != nil {
		return 0, err
	}
	transaction.revision = revision
	return revision, nil
}
