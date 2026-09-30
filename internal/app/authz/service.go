// Package authz checks permission keys against a startup catalog and resolves user grants fresh on every request. Catalog changes require restart (ADR-0008).
package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// New rejects a missing resolver or empty startup catalog before authorization can run.
func New(resolver PermissionResolver, catalog []identity.Permission) (*Service, error) {
	if resolver == nil {
		return nil, errors.New("authz: nil permission resolver")
	}
	if len(catalog) == 0 {
		return nil, errors.New("authz: empty permission catalog (is the database seeded? — migration 0002)")
	}
	known := make(map[identity.Permission]struct{}, len(catalog))
	for _, p := range catalog {
		known[p] = struct{}{}
	}
	return &Service{resolver: resolver, known: known}, nil
}

// Authorize distinguishes unknown catalog keys, resolver failures, and genuine denial; only the last returns ErrPermissionDenied.
func (s *Service) Authorize(ctx context.Context, user identity.User, want identity.Permission) error {
	if _, ok := s.known[want]; !ok {
		return fmt.Errorf("authz: %q: %w", want, ErrUnknownPermission)
	}
	org, err := s.resolver.DefaultOrganizationID(ctx)
	if err != nil {
		return fmt.Errorf("authz: resolve organization: %w", err)
	}
	perms, err := s.resolver.PermissionsForUser(ctx, org, user.ID)
	if err != nil {
		return fmt.Errorf("authz: resolve permissions: %w", err)
	}
	if slices.Contains(perms, want) {
		return nil
	}
	return ErrPermissionDenied
}

// SessionInfo returns sorted permission keys and a role label from one organization lookup. They are advisory; every RPC still authorizes independently.
func (s *Service) SessionInfo(ctx context.Context, user identity.User) ([]identity.Permission, string, error) {
	org, err := s.resolver.DefaultOrganizationID(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("authz: resolve organization: %w", err)
	}
	perms, err := s.resolver.PermissionsForUser(ctx, org, user.ID)
	if err != nil {
		return nil, "", fmt.Errorf("authz: resolve permissions: %w", err)
	}
	slices.Sort(perms)
	role, err := s.resolver.RoleNameForUser(ctx, org, user.ID)
	if err != nil {
		return nil, "", fmt.Errorf("authz: resolve role name: %w", err)
	}
	return perms, role, nil
}

// LoadCatalog reads startup permission keys and rejects an unseeded, empty catalog.
func LoadCatalog(ctx context.Context, src CatalogSource) ([]identity.Permission, error) {
	perms, err := src.ListPermissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("authz: load permission catalog: %w", err)
	}
	if len(perms) == 0 {
		return nil, errors.New("authz: permission catalog is empty (run migration 0002 seed)")
	}
	return perms, nil
}
