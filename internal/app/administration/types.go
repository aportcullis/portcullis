package administration

import (
	"time"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// UserService implements the user administration use cases (ADR-0053).
type UserService struct {
	repo  UserRepository
	guard *delegationGuard
}

// RoleService implements the custom-role administration use cases over the startup permission catalog (ADR-0053).
type RoleService struct {
	repo    RoleRepository
	guard   *delegationGuard
	catalog []identity.Permission
}

// CreateUserParams is an administrator's request to create an account with an initial role.
type CreateUserParams struct {
	Email       string
	DisplayName string
	RoleID      identity.RoleID
}

// SetupLink is a freshly issued one-time password setup token; the raw token is returned once and never stored.
type SetupLink struct {
	Token     string
	ExpiresAt time.Time
}

// CreatedUser is the new member together with its setup link.
type CreatedUser struct {
	Member    identity.Member
	SetupLink SetupLink
}

// RoleParams is a full custom-role definition as submitted by an administrator.
type RoleParams struct {
	Name        string
	Permissions []identity.Permission
}
