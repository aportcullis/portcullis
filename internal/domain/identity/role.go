package identity

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// MaxRoleNameLength bounds a role name in Unicode code points (ADR-0053).
const MaxRoleNameLength = 64

// Role refusals (ADR-0053).
var (
	ErrRoleNotFound = errors.New("identity: role not found")

	ErrInvalidRoleName = errors.New("identity: invalid role name")

	ErrRoleNameTaken = errors.New("identity: role name already in use")

	// ErrPermissionNotInCatalog means a role definition named a key the loaded permission catalog lacks.
	ErrPermissionNotInCatalog = errors.New("identity: permission not in catalog")

	// ErrSystemRoleImmutable means a seeded system role was targeted by a rename, permission change or delete.
	ErrSystemRoleImmutable = errors.New("identity: system roles are read-only")

	// ErrRoleInUse means a role still assigned to a membership was targeted by a delete.
	ErrRoleInUse = errors.New("identity: role is assigned to members")

	// ErrRoleConflict means the role changed after the caller read it.
	ErrRoleConflict = errors.New("identity: role changed; refresh and retry")
)

// Role is a named bundle of permissions. Roles live in the database; the seeded system roles are defaults, not a closed set — admins create custom roles too.
type Role struct {
	ID          RoleID
	Name        string
	IsSystem    bool
	Permissions []Permission
	// Version is the optimistic concurrency token a role update must present (ADR-0053).
	Version int64
	// MemberCount is the number of memberships assigned this role, reported for administration views.
	MemberCount int64
}

// RoleDefinition is a validated custom-role name and its sorted, de-duplicated permission keys.
type RoleDefinition struct {
	Name        string
	Permissions []Permission
}

// Has reports whether the role grants the permission.
func (r Role) Has(permission Permission) bool {
	return slices.Contains(r.Permissions, permission)
}

// NewRoleDefinition trims and validates a custom-role name and requires every permission key to exist in the loaded catalog.
func NewRoleDefinition(name string, permissions, catalog []Permission) (RoleDefinition, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxRoleNameLength || hasUnsafeDisplayRune(name) {
		return RoleDefinition{}, ErrInvalidRoleName
	}
	for _, permission := range permissions {
		if !slices.Contains(catalog, permission) {
			return RoleDefinition{}, fmt.Errorf("%w: %q", ErrPermissionNotInCatalog, permission)
		}
	}
	return RoleDefinition{Name: name, Permissions: sortedUniquePermissions(permissions)}, nil
}

// DiffPermissions returns the sorted keys present only in next (added) and only in previous (removed).
func DiffPermissions(previous, next []Permission) (added, removed []Permission) {
	for _, permission := range sortedUniquePermissions(next) {
		if !slices.Contains(previous, permission) {
			added = append(added, permission)
		}
	}
	for _, permission := range sortedUniquePermissions(previous) {
		if !slices.Contains(next, permission) {
			removed = append(removed, permission)
		}
	}
	return added, removed
}

// sortedUniquePermissions returns a sorted copy without duplicates; an empty input stays nil.
func sortedUniquePermissions(permissions []Permission) []Permission {
	if len(permissions) == 0 {
		return nil
	}
	sorted := slices.Clone(permissions)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}
