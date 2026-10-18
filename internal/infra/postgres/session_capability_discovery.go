package postgres

import (
	"context"
	"errors"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/jackc/pgx/v5/pgtype"
)

// discoverCapability stops at an evaluated grant and never interprets a truncated batch as absence.
func (store *BindingPolicyStore) discoverCapability(ctx context.Context, queries *db.Queries, grants *resourceGrantCache, permission identity.Permission) (bool, error) {
	organization, err := policyOrganizationUUID(store.organization)
	if err != nil {
		return false, err
	}
	user, valid := parseBindingPolicyUUID(string(grants.scope.operation.Principal))
	if !valid {
		return false, authz.ErrPermissionDenied
	}
	for _, kind := range authz.ResourceTypesForPermission(permission) {
		var after pgtype.UUID
		for {
			candidates, err := queries.ListCapabilityTargets(ctx, db.ListCapabilityTargetsParams{OrganizationID: organization, UserID: user, PermissionKey: string(permission), ResourceType: string(kind), EvaluatedAt: timeToTS(grants.scope.at), AfterID: after, CandidateLimit: int32(store.policy.readSettings.MaxCandidates)}) //nolint:gosec // constructor validates the 65536 ceiling
			if err != nil {
				return false, err
			}
			for _, candidate := range candidates {
				if !candidate.AncestryUnambiguous {
					return false, authz.ErrInvalidSnapshot
				}
				resource := identity.ResourceReference{OrganizationID: store.organization, Type: kind, ID: uuidToString(candidate.ID), ConnectionID: uuidToString(candidate.ConnectionID)}
				if _, err := grants.authorize(ctx, resource, permission); err == nil {
					return true, nil
				} else if !errors.Is(err, authz.ErrPermissionDenied) {
					return false, err
				}
				after = candidate.ID
			}
			if len(candidates) < store.policy.readSettings.MaxCandidates {
				break
			}
		}
	}
	return false, nil
}
