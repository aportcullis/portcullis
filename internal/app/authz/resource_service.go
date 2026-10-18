package authz

import (
	"context"
	"errors"
	"time"

	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// ResourceService evaluates grants independently of the legacy membership resolver.
type ResourceService struct {
	repository ResourceRepository
	conditions ResourceConditions
	known      map[identity.Permission]struct{}
	settings   ResourceConfig
}

// ResourceDecision records a permission checkpoint and the granting versions, not workflow admission.
type ResourceDecision struct {
	Resource       identity.ResourceReference
	EvaluatedAt    time.Time
	Revision       int64
	BindingID      identity.BindingID
	BindingVersion int64
	RoleVersion    int64
}

// Resource authorization errors deliberately omit storage details and condition source.
var (
	ErrAuthorizationInactive = errors.New("authz: binding authorization inactive")
	ErrSnapshotUnavailable   = errors.New("authz: authorization snapshot unavailable")
	ErrInvalidSnapshot       = errors.New("authz: invalid authorization snapshot")
	ErrBindingLimitExceeded  = errors.New("authz: binding limit exceeded")
)

// NewResourceService checks dependencies, catalog and finite decision limits.
func NewResourceService(repository ResourceRepository, conditions ResourceConditions, catalog []identity.Permission, settings ResourceConfig) (*ResourceService, error) {
	if repository == nil || conditions == nil || len(catalog) == 0 || settings.Validate() != nil {
		return nil, ErrInvalidSnapshot
	}
	known := make(map[identity.Permission]struct{}, len(catalog))
	for _, permission := range catalog {
		known[permission] = struct{}{}
	}
	return &ResourceService{repository: repository, conditions: conditions, known: known, settings: settings}, nil
}

// AuthorizeResource resolves trusted attributes and returns one additive grant checkpoint.
func (s *ResourceService) AuthorizeResource(ctx context.Context, principal identity.UserID, permission identity.Permission, target ResourceTarget) (ResourceDecision, error) {
	ctx, cancel := context.WithTimeout(ctx, s.settings.Timeout)
	defer cancel()
	if err := resourceContextError(ctx); err != nil {
		return ResourceDecision{}, err
	}
	if _, known := s.known[permission]; !known {
		return ResourceDecision{}, ErrUnknownPermission
	}
	if principal == "" || target.OrganizationID == "" || target.ID == "" || !supportsResourcePermission(permission, target.Type) {
		return ResourceDecision{}, ErrPermissionDenied
	}
	snapshot, err := s.repository.LoadAuthorizationSnapshot(ctx, principal, permission, target, s.settings.MaxBindings)
	if err := resourceContextError(ctx); err != nil {
		return ResourceDecision{}, err
	}
	if err != nil {
		for _, safe := range []error{ErrPermissionDenied, ErrAuthorizationInactive, ErrBindingLimitExceeded, ErrInvalidSnapshot} {
			if errors.Is(err, safe) {
				return ResourceDecision{}, safe
			}
		}
		return ResourceDecision{}, ErrSnapshotUnavailable
	}
	if !snapshot.ActiveMember {
		return ResourceDecision{}, ErrPermissionDenied
	}
	if snapshot.Resource.Validate() != nil || snapshot.Resource.OrganizationID != target.OrganizationID || snapshot.Resource.Type != target.Type || snapshot.Resource.ID != target.ID || snapshot.EvaluatedAt.IsZero() || snapshot.Revision <= 0 {
		return ResourceDecision{}, ErrInvalidSnapshot
	}
	if len(snapshot.Grants) > s.settings.MaxBindings {
		return ResourceDecision{}, ErrBindingLimitExceeded
	}
	// Validate the entire returned set before considering any prefix grant.
	for _, grant := range snapshot.Grants {
		validation := grant.Binding.Validate()
		if (validation != nil && !errors.Is(validation, identity.ErrInvalidCondition)) || grant.RoleVersion <= 0 || grant.Permission != permission || grant.Binding.OrganizationID != target.OrganizationID || grant.Binding.UserID != principal {
			return ResourceDecision{}, ErrInvalidSnapshot
		}
	}
	for _, grant := range snapshot.Grants {
		if err := resourceContextError(ctx); err != nil {
			return ResourceDecision{}, err
		}
		if !grant.Binding.AppliesTo(principal, snapshot.Resource) {
			continue
		}
		if requiresUnconditionalManagement(permission) && (grant.Binding.HasCondition || grant.Binding.Scope.Type != identity.ScopeOrganization) {
			continue
		}
		if grant.Binding.HasCondition {
			checked, prepareErr := s.conditions.Prepare(ctx, grant.Binding.Condition)
			if prepareErr != nil {
				continue
			}
			result := s.conditions.Evaluate(ctx, checked, condition.AttributeSnapshot{PrincipalID: principal, PrincipalOrganizationID: target.OrganizationID, Resource: snapshot.Resource, RequestTime: snapshot.EvaluatedAt})
			if result.Outcome != condition.Match {
				continue
			}
		}
		if err := resourceContextError(ctx); err != nil {
			return ResourceDecision{}, err
		}
		return ResourceDecision{Resource: snapshot.Resource, EvaluatedAt: snapshot.EvaluatedAt, Revision: snapshot.Revision, BindingID: grant.Binding.ID, BindingVersion: grant.Binding.Version, RoleVersion: grant.RoleVersion}, nil
	}
	if err := resourceContextError(ctx); err != nil {
		return ResourceDecision{}, err
	}
	return ResourceDecision{}, ErrPermissionDenied
}

// resourceContextError fences elapsed deadlines even before cancellation signals arrive.
func resourceContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, exists := ctx.Deadline(); exists && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}
