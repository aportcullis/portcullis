// Package authz is the authorization application service: it decides whether an
// authenticated user holds a required permission (has(permission), ADR-0008).
// Permissions are DB-catalog keys — the service never enumerates role names. The
// valid-key catalog is loaded once at startup (LoadCatalog) and snapshotted for the
// process lifetime; a user's effective permissions are resolved fresh from the
// database on every Authorize call (no cache), so a revoked grant takes effect on
// the next request and there is no invalidation logic to get wrong. The per-request
// cost matches the auth interceptor, which already reads the session on every call.
// A catalog key added by a later migration is picked up on the next restart
// (deploy) — the only time the seeded catalog changes.
package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// New builds the service from a per-request permission resolver and the catalog
// snapshot loaded at startup (see LoadCatalog). It fails if the resolver is nil or
// the catalog is empty: an empty catalog means the database was never seeded
// (migration 0002), which would make every check an ErrUnknownPermission, so
// refusing here turns that into a fail-fast at construction rather than a runtime
// surprise on the first authorized request.
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

// Authorize returns nil iff the user's effective permissions include want.
//
// The ordering is deliberate:
//   - want must be a known catalog key first; an unknown key is a defect at the
//     enforcement site (a typo, or a key dropped from the catalog), returned as
//     ErrUnknownPermission so it surfaces as an internal error — never as a silent
//     denial that no user could ever satisfy.
//   - a resolver (database) error is wrapped and returned as-is, NOT turned into a
//     denial: a DB blip must not read as "forbidden" (the same rule the auth
//     interceptor applies to session lookups).
//   - only an authenticated user who genuinely lacks the permission gets
//     ErrPermissionDenied.
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

// PermissionsFor returns the user's effective permission keys, sorted. It
// feeds the SPA's affordance gating (Me/Login responses — ADR-0014's deferred
// Me.permissions): hiding is UX only, every RPC still calls Authorize. Role
// changes revoke sessions immediately (§8.3), so a login/Me-time snapshot
// cannot go stale within a session.
func (s *Service) PermissionsFor(ctx context.Context, user identity.User) ([]identity.Permission, error) {
	org, err := s.resolver.DefaultOrganizationID(ctx)
	if err != nil {
		return nil, fmt.Errorf("authz: resolve organization: %w", err)
	}
	perms, err := s.resolver.PermissionsForUser(ctx, org, user.ID)
	if err != nil {
		return nil, fmt.Errorf("authz: resolve permissions: %w", err)
	}
	slices.Sort(perms)
	return perms, nil
}

// LoadCatalog reads the seeded permission catalog from src. It is called once at
// startup and its result is passed to New. An empty catalog is returned as an error
// (the database is unseeded), so the process refuses to start rather than run with
// authorization that can never grant anything — matching the keyring and
// runtime-connection fail-fast checks at boot.
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
