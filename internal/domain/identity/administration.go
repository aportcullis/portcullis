package identity

import (
	"errors"
	"slices"
)

// Administration refusals (ADR-0053).
var (
	// ErrPrivilegeEscalation means the actor tried to grant, strip or lock out a permission it does not hold.
	ErrPrivilegeEscalation = errors.New("identity: permission not held by the actor")

	// ErrSelfAdministration means the actor tried to disable, enable or reassign its own account.
	ErrSelfAdministration = errors.New("identity: cannot administer your own account")

	// ErrLastAdministrator means the change would leave the organization without an active administrator.
	ErrLastAdministrator = errors.New("identity: change would remove the last active administrator")

	// ErrActorNotAuthorized means the actor was disabled or lost the permission that admitted the request.
	ErrActorNotAuthorized = errors.New("identity: actor is no longer authorized")
)

// Delegation is the actor and the permission (Gate) an administration request was admitted with (ADR-0053).
type Delegation struct {
	Actor UserID
	Gate  Permission
}

// Authorize returns ErrActorNotAuthorized unless the actor is active and holds Gate, then ValidateDelegation over the affected sets.
func (d Delegation) Authorize(actorActive bool, held []Permission, affected ...[]Permission) error {
	if !actorActive || !slices.Contains(held, d.Gate) {
		return ErrActorNotAuthorized
	}
	return ValidateDelegation(held, affected...)
}

// AdministratorPermissions returns the permission keys that together make an active member an administrator (ADR-0008).
func AdministratorPermissions() []Permission {
	return []Permission{PermissionUsersUpdate, PermissionUsersDisable}
}

// IsAdministrator reports whether a permission set holds every administrator permission.
func IsAdministrator(permissions []Permission) bool {
	return holdsEvery(permissions, AdministratorPermissions())
}

// ValidateDelegation returns ErrPrivilegeEscalation unless the actor holds every permission in each affected set, so nobody grants, strips or locks out access beyond their own (ADR-0053).
func ValidateDelegation(actor []Permission, affected ...[]Permission) error {
	for _, permissions := range affected {
		if !holdsEvery(actor, permissions) {
			return ErrPrivilegeEscalation
		}
	}
	return nil
}

// ValidateNotSelf returns ErrSelfAdministration when the actor targets its own account.
func ValidateNotSelf(actor, target UserID) error {
	if actor == target {
		return ErrSelfAdministration
	}
	return nil
}

// holdsEvery reports whether held contains every key in required.
func holdsEvery(held, required []Permission) bool {
	for _, permission := range required {
		if !slices.Contains(held, permission) {
			return false
		}
	}
	return true
}
