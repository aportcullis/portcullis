package authz

import "github.com/aportcullis/portcullis/internal/domain/identity"

// Service answers has(permission) for authenticated users (ADR-0008). It holds a
// startup snapshot of the permission catalog (the valid-key universe) and resolves
// each user's effective permissions fresh per request via the injected resolver.
// Its constructor (New) and methods live in service.go (file-split convention).
type Service struct {
	resolver PermissionResolver
	known    map[identity.Permission]struct{}
}
