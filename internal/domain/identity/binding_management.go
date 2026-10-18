package identity

import (
	"errors"
	"strings"
)

// Binding permission names remain outside the active enforcement catalog until atomic activation.
const (
	PermissionBindingsList   Permission = "bindings.list"
	PermissionBindingsGet    Permission = "bindings.get"
	PermissionBindingsCreate Permission = "bindings.create"
	PermissionBindingsUpdate Permission = "bindings.update"
	PermissionBindingsDelete Permission = "bindings.delete"
)

// Binding management refusals preserve optimistic and immutable history boundaries.
var (
	ErrBindingNotFound = errors.New("identity: binding not found")
	ErrBindingConflict = errors.New("identity: binding changed; refresh and retry")
	ErrBindingRevoked  = errors.New("identity: binding is revoked")
)

// BindingDefinition describes a grant without assigning persistence identity or version.
type BindingDefinition struct {
	UserID       UserID
	RoleID       RoleID
	Scope        ResourceScope
	Condition    Condition
	HasCondition bool
}

// Validate rejects incomplete grants and implicit condition clearing.
func (definition BindingDefinition) Validate() error {
	if definition.UserID == "" || definition.RoleID == "" || definition.Scope.Validate() != nil {
		return ErrInvalidRoleBinding
	}
	if definition.HasCondition {
		return definition.Condition.Validate()
	}
	if definition.Condition != (Condition{}) {
		return ErrInvalidCondition
	}
	return nil
}

// Definition copies structured grant data without history identifiers.
func (binding RoleBinding) Definition() BindingDefinition {
	return BindingDefinition{UserID: binding.UserID, RoleID: binding.RoleID, Scope: binding.Scope, Condition: binding.Condition, HasCondition: binding.HasCondition}
}

// ConditionEditMode distinguishes omitted edits from explicit replacement and removal.
type ConditionEditMode string

const (
	ConditionPreserve ConditionEditMode = ""
	ConditionReplace  ConditionEditMode = "replace"
	ConditionClear    ConditionEditMode = "clear"
)

// BindingUpdate changes a role or scope while preserving the immutable subject.
type BindingUpdate struct {
	RoleID        RoleID
	Scope         *ResourceScope
	ConditionMode ConditionEditMode
	Condition     Condition
}

// Apply preserves omitted fields and refuses edits to unsupported existing condition contracts.
func (update BindingUpdate) Apply(current RoleBinding) (BindingDefinition, error) {
	if err := current.Validate(); err != nil {
		return BindingDefinition{}, err
	}
	next := current.Definition()
	if update.RoleID != "" {
		next.RoleID = update.RoleID
	}
	if update.Scope != nil {
		next.Scope = *update.Scope
	}
	switch update.ConditionMode {
	case ConditionPreserve:
		if update.Condition != (Condition{}) {
			return BindingDefinition{}, ErrInvalidCondition
		}
	case ConditionReplace:
		next.Condition = update.Condition
		next.HasCondition = true
	case ConditionClear:
		if update.Condition != (Condition{}) {
			return BindingDefinition{}, ErrInvalidCondition
		}
		next.Condition = Condition{}
		next.HasCondition = false
	default:
		return BindingDefinition{}, ErrInvalidCondition
	}
	return next, next.Validate()
}

// BindingManagementPermissions returns the planned binding policy catalog keys.
func BindingManagementPermissions() []Permission {
	return []Permission{PermissionBindingsList, PermissionBindingsGet, PermissionBindingsCreate, PermissionBindingsUpdate, PermissionBindingsDelete}
}

// SelectRecoveryPermissions selects every administration capability from the authoritative catalog.
func SelectRecoveryPermissions(catalog []Permission) []Permission {
	required := make([]Permission, 0, len(catalog))
	for _, permission := range catalog {
		subject, _, _ := strings.Cut(string(permission), ".")
		if subject == "users" || subject == "roles" || subject == "bindings" {
			required = append(required, permission)
		}
	}
	return sortedUniquePermissions(required)
}
