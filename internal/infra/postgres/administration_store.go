package postgres

import (
	"context"
	"errors"
	"maps"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// rolesOrgNameIndex is the partial unique index on live role names per organization.
const rolesOrgNameIndex = "roles_org_name"

// withAdministrationTx runs fn under the per-organization administration lock with the instant observed after acquiring it (ADR-0009/0053).
func (s *IdentityStore) withAdministrationTx(ctx context.Context, org identity.OrganizationID, fn func(q *db.Queries, orgID pgtype.UUID, at time.Time) error) error {
	orgID, err := stringToUUID(string(org))
	if err != nil {
		return identity.ErrUserNotFound
	}
	return s.withLockedTx(ctx, lockClassAdministration, organizationLockObject(org), func(q *db.Queries) error {
		at, err := observeInstant(ctx, q)
		if err != nil {
			return err
		}
		return fn(q, orgID, at)
	})
}

// Per docs/adr/0053-user-and-role-administration.md (Safeguards), re-checks the delegation against the actor's state read in this locked transaction.
func reauthorizeDelegation(ctx context.Context, queries *db.Queries, orgID pgtype.UUID, delegation identity.Delegation, affected ...[]identity.Permission) error {
	actorID, err := stringToUUID(string(delegation.Actor))
	if err != nil {
		return identity.ErrActorNotAuthorized
	}
	authority, err := queries.GetActorAuthority(ctx, db.GetActorAuthorityParams{OrganizationID: orgID, UserID: actorID})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrActorNotAuthorized
	}
	if err != nil {
		return err
	}
	return delegation.Authorize(identity.UserStatus(authority.Status) == identity.StatusActive, toPermissions(authority.Permissions), affected...)
}

func lockedMemberRolePermissions(ctx context.Context, queries *db.Queries, org identity.OrganizationID, user identity.UserID) ([]identity.Permission, error) {
	member, err := getMember(ctx, queries, org, user)
	if err != nil {
		return nil, err
	}
	role, err := getRole(ctx, queries, org, member.RoleID)
	if err != nil {
		return nil, err
	}
	return role.Permissions, nil
}

// requireActiveAdministrator rolls the transaction back with ErrLastAdministrator when no active member's role holds every administrator permission.
func requireActiveAdministrator(ctx context.Context, q *db.Queries, orgID pgtype.UUID) error {
	administrators, err := q.CountActiveAdministrators(ctx, db.CountActiveAdministratorsParams{
		OrganizationID:           orgID,
		AdministratorPermissions: permissionStrings(identity.AdministratorPermissions()),
	})
	if err != nil {
		return err
	}
	if administrators == 0 {
		return identity.ErrLastAdministrator
	}
	return nil
}

// commitAdministrationEvent stamps the event with the observed instant and organization, merges store-side metadata, and inserts it in the transaction.
func commitAdministrationEvent(ctx context.Context, q *db.Queries, event audit.Event, org identity.OrganizationID, at time.Time, targetID string, extra map[string]any) error {
	event.OrganizationID = org
	event.OccurredAt = at
	if targetID != "" {
		event.TargetID = targetID
	}
	if len(extra) > 0 {
		metadata := make(map[string]any, len(event.Metadata)+len(extra))
		maps.Copy(metadata, event.Metadata)
		maps.Copy(metadata, extra)
		event.Metadata = metadata
	}
	return insertAuditTx(ctx, q, event)
}

func permissionStrings(permissions []identity.Permission) []string {
	keys := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		keys = append(keys, string(permission))
	}
	return keys
}

func toPermissions(keys []string) []identity.Permission {
	if len(keys) == 0 {
		return nil
	}
	permissions := make([]identity.Permission, 0, len(keys))
	for _, key := range keys {
		permissions = append(permissions, identity.Permission(key))
	}
	return permissions
}

func toMember(row db.GetMemberRow) identity.Member {
	return identity.Member{
		User: identity.User{
			ID:          identity.UserID(uuidToString(row.ID)),
			Email:       row.Email,
			DisplayName: row.DisplayName,
			Status:      identity.UserStatus(row.Status),
			CreatedAt:   tsToTime(row.CreatedAt),
		},
		RoleID:      identity.RoleID(uuidToString(row.RoleID)),
		RoleName:    row.RoleName,
		HasPassword: row.HasPassword,
	}
}

func toRole(row db.GetRoleWithPermissionsRow) identity.Role {
	return identity.Role{
		ID:          identity.RoleID(uuidToString(row.ID)),
		Name:        row.Name,
		IsSystem:    row.IsSystem,
		Permissions: toPermissions(row.Permissions),
		Version:     row.Version,
		MemberCount: row.MemberCount,
	}
}

// memberKeys parses the organization and user ids, mapping a malformed id to ErrUserNotFound.
func memberKeys(org identity.OrganizationID, user identity.UserID) (pgtype.UUID, pgtype.UUID, error) {
	orgID, err := stringToUUID(string(org))
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, identity.ErrUserNotFound
	}
	userID, err := stringToUUID(string(user))
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, identity.ErrUserNotFound
	}
	return orgID, userID, nil
}

// ListMembers returns the organization's members ordered by email.
func (s *IdentityStore) ListMembers(ctx context.Context, org identity.OrganizationID) ([]identity.Member, error) {
	orgID, err := stringToUUID(string(org))
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListMembers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	members := make([]identity.Member, 0, len(rows))
	for _, row := range rows {
		members = append(members, toMember(db.GetMemberRow(row)))
	}
	return members, nil
}

// GetMember returns one member of the organization.
func (s *IdentityStore) GetMember(ctx context.Context, org identity.OrganizationID, user identity.UserID) (identity.Member, error) {
	return getMember(ctx, s.q, org, user)
}

func getMember(ctx context.Context, q *db.Queries, org identity.OrganizationID, user identity.UserID) (identity.Member, error) {
	orgID, userID, err := memberKeys(org, user)
	if err != nil {
		return identity.Member{}, err
	}
	row, err := q.GetMember(ctx, db.GetMemberParams{OrganizationID: orgID, UserID: userID})
	if err != nil {
		return identity.Member{}, notFound(err, identity.ErrUserNotFound)
	}
	return toMember(row), nil
}

// GetRole returns one live role of the organization with its permissions and member count.
func (s *IdentityStore) GetRole(ctx context.Context, org identity.OrganizationID, role identity.RoleID) (identity.Role, error) {
	return getRole(ctx, s.q, org, role)
}

func getRole(ctx context.Context, q *db.Queries, org identity.OrganizationID, role identity.RoleID) (identity.Role, error) {
	orgID, err := stringToUUID(string(org))
	if err != nil {
		return identity.Role{}, identity.ErrRoleNotFound
	}
	roleID, err := stringToUUID(string(role))
	if err != nil {
		return identity.Role{}, identity.ErrRoleNotFound
	}
	row, err := q.GetRoleWithPermissions(ctx, db.GetRoleWithPermissionsParams{OrganizationID: orgID, RoleID: roleID})
	if err != nil {
		return identity.Role{}, notFound(err, identity.ErrRoleNotFound)
	}
	return toRole(row), nil
}

// ListRoles returns the organization's live roles, system roles first.
func (s *IdentityStore) ListRoles(ctx context.Context, org identity.OrganizationID) ([]identity.Role, error) {
	orgID, err := stringToUUID(string(org))
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListRolesWithPermissions(ctx, orgID)
	if err != nil {
		return nil, err
	}
	roles := make([]identity.Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, toRole(db.GetRoleWithPermissionsRow(row)))
	}
	return roles, nil
}

// CreateMember inserts the user, membership and open setup link with the audit event, under the administration lock so a concurrent delete cannot remove the role.
func (s *IdentityStore) CreateMember(ctx context.Context, org identity.OrganizationID, delegation identity.Delegation, member identity.NewMember, setup identity.PasswordSetupIssue, event audit.Event) (identity.Member, identity.PasswordSetup, error) {
	var created identity.Member
	var issued identity.PasswordSetup
	err := s.withAdministrationTx(ctx, org, func(q *db.Queries, orgID pgtype.UUID, at time.Time) error {
		role, err := getRole(ctx, q, org, member.RoleID)
		if err != nil {
			return err
		}
		if err := reauthorizeDelegation(ctx, q, orgID, delegation, role.Permissions); err != nil {
			return err
		}
		roleID, err := stringToUUID(string(role.ID))
		if err != nil {
			return err
		}
		user, err := q.CreateUser(ctx, db.CreateUserParams{Email: member.Email, DisplayName: member.DisplayName})
		if err != nil {
			return onUniqueViolation(err, usersEmailLowerIndex, identity.ErrEmailTaken)
		}
		if _, err := q.CreateMembership(ctx, db.CreateMembershipParams{OrganizationID: orgID, UserID: user.ID, RoleID: roleID}); err != nil {
			return err
		}
		expiresAt, err := q.InsertPasswordSetupToken(ctx, db.InsertPasswordSetupTokenParams{
			OrganizationID: orgID, UserID: user.ID, TokenHash: setup.TokenHash, At: timeToTS(at), ValiditySeconds: setup.Validity.Seconds(),
		})
		if err != nil {
			return err
		}
		userID := identity.UserID(uuidToString(user.ID))
		if err := commitAdministrationEvent(ctx, q, event, org, at, string(userID), nil); err != nil {
			return err
		}
		issued = identity.PasswordSetup{UserID: userID, OrganizationID: org, ExpiresAt: tsToTime(expiresAt)}
		created, err = getMember(ctx, q, org, userID)
		return err
	})
	return created, issued, err
}

// IssuePasswordSetup replaces the user's open setup link with the audit event; disabled users and users with a password are refused.
func (s *IdentityStore) IssuePasswordSetup(ctx context.Context, org identity.OrganizationID, delegation identity.Delegation, user identity.UserID, setup identity.PasswordSetupIssue, event audit.Event) (identity.PasswordSetup, error) {
	var issued identity.PasswordSetup
	err := s.withAdministrationTx(ctx, org, func(q *db.Queries, orgID pgtype.UUID, at time.Time) error {
		userID, state, err := lockMember(ctx, q, orgID, user)
		if err != nil {
			return err
		}
		current, err := lockedMemberRolePermissions(ctx, q, org, user)
		if err != nil {
			return err
		}
		if err := reauthorizeDelegation(ctx, q, orgID, delegation, current); err != nil {
			return err
		}
		switch {
		case identity.UserStatus(state.Status) != identity.StatusActive:
			return identity.ErrUserDisabled
		case state.HasPassword:
			return identity.ErrPasswordAlreadySet
		}
		if err := q.RevokeOpenPasswordSetups(ctx, db.RevokeOpenPasswordSetupsParams{At: timeToTS(at), UserID: userID}); err != nil {
			return err
		}
		expiresAt, err := q.InsertPasswordSetupToken(ctx, db.InsertPasswordSetupTokenParams{
			OrganizationID: orgID, UserID: userID, TokenHash: setup.TokenHash, At: timeToTS(at), ValiditySeconds: setup.Validity.Seconds(),
		})
		if err != nil {
			return err
		}
		issued = identity.PasswordSetup{UserID: user, OrganizationID: org, ExpiresAt: tsToTime(expiresAt)}
		return commitAdministrationEvent(ctx, q, event, org, at, string(user), nil)
	})
	return issued, err
}

// lockMember locks the account and membership of a user in the organization.
func lockMember(ctx context.Context, q *db.Queries, orgID pgtype.UUID, user identity.UserID) (pgtype.UUID, db.LockMemberRow, error) {
	userID, err := stringToUUID(string(user))
	if err != nil {
		return pgtype.UUID{}, db.LockMemberRow{}, identity.ErrUserNotFound
	}
	state, err := q.LockMember(ctx, db.LockMemberParams{OrganizationID: orgID, UserID: userID})
	if err != nil {
		return pgtype.UUID{}, db.LockMemberRow{}, notFound(err, identity.ErrUserNotFound)
	}
	return userID, state, nil
}

// SetMemberStatus changes the account status, revokes its sessions (and on disable its open setup link), and rolls back when no active administrator would remain.
func (s *IdentityStore) SetMemberStatus(ctx context.Context, org identity.OrganizationID, delegation identity.Delegation, user identity.UserID, status identity.UserStatus, event audit.Event) (identity.Member, error) {
	var updated identity.Member
	err := s.withAdministrationTx(ctx, org, func(q *db.Queries, orgID pgtype.UUID, at time.Time) error {
		userID, _, err := lockMember(ctx, q, orgID, user)
		if err != nil {
			return err
		}
		current, err := lockedMemberRolePermissions(ctx, q, org, user)
		if err != nil {
			return err
		}
		if err := reauthorizeDelegation(ctx, q, orgID, delegation, current); err != nil {
			return err
		}
		if err := q.SetUserStatus(ctx, db.SetUserStatusParams{Status: string(status), UserID: userID}); err != nil {
			return err
		}
		if status == identity.StatusDisabled {
			if err := q.RevokeOpenPasswordSetups(ctx, db.RevokeOpenPasswordSetupsParams{At: timeToTS(at), UserID: userID}); err != nil {
				return err
			}
		}
		revoked, err := q.RevokeUserSessionsAt(ctx, db.RevokeUserSessionsAtParams{At: timeToTS(at), UserID: userID})
		if err != nil {
			return err
		}
		if err := requireActiveAdministrator(ctx, q, orgID); err != nil {
			return err
		}
		if err := commitAdministrationEvent(ctx, q, event, org, at, string(user), map[string]any{"revoked_sessions": revoked}); err != nil {
			return err
		}
		updated, err = getMember(ctx, q, org, user)
		return err
	})
	return updated, err
}

// AssignMemberRole replaces the membership role, revokes the member's sessions, and rolls back when no active administrator would remain.
func (s *IdentityStore) AssignMemberRole(ctx context.Context, org identity.OrganizationID, delegation identity.Delegation, user identity.UserID, role identity.RoleID, event audit.Event) (identity.Member, error) {
	var updated identity.Member
	err := s.withAdministrationTx(ctx, org, func(q *db.Queries, orgID pgtype.UUID, at time.Time) error {
		userID, _, err := lockMember(ctx, q, orgID, user)
		if err != nil {
			return err
		}
		live, err := getRole(ctx, q, org, role)
		if err != nil {
			return err
		}
		current, err := lockedMemberRolePermissions(ctx, q, org, user)
		if err != nil {
			return err
		}
		if err := reauthorizeDelegation(ctx, q, orgID, delegation, current, live.Permissions); err != nil {
			return err
		}
		roleID, err := stringToUUID(string(live.ID))
		if err != nil {
			return err
		}
		if err := q.SetMembershipRole(ctx, db.SetMembershipRoleParams{RoleID: roleID, OrganizationID: orgID, UserID: userID}); err != nil {
			return err
		}
		revoked, err := q.RevokeUserSessionsAt(ctx, db.RevokeUserSessionsAtParams{At: timeToTS(at), UserID: userID})
		if err != nil {
			return err
		}
		if err := requireActiveAdministrator(ctx, q, orgID); err != nil {
			return err
		}
		if err := commitAdministrationEvent(ctx, q, event, org, at, string(user), map[string]any{"revoked_sessions": revoked}); err != nil {
			return err
		}
		updated, err = getMember(ctx, q, org, user)
		return err
	})
	return updated, err
}

// CreateRole inserts a custom role and its permissions with the audit event.
func (s *IdentityStore) CreateRole(ctx context.Context, org identity.OrganizationID, delegation identity.Delegation, definition identity.RoleDefinition, event audit.Event) (identity.Role, error) {
	var created identity.Role
	err := s.withAdministrationTx(ctx, org, func(q *db.Queries, orgID pgtype.UUID, at time.Time) error {
		if err := reauthorizeDelegation(ctx, q, orgID, delegation, definition.Permissions); err != nil {
			return err
		}
		roleID, err := q.InsertRole(ctx, db.InsertRoleParams{OrganizationID: orgID, Name: definition.Name, At: timeToTS(at)})
		if err != nil {
			return onUniqueViolation(err, rolesOrgNameIndex, identity.ErrRoleNameTaken)
		}
		if err := q.GrantRolePermissions(ctx, db.GrantRolePermissionsParams{RoleID: roleID, PermissionKeys: permissionStrings(definition.Permissions)}); err != nil {
			return err
		}
		id := identity.RoleID(uuidToString(roleID))
		if err := commitAdministrationEvent(ctx, q, event, org, at, string(id), nil); err != nil {
			return err
		}
		created, err = getRole(ctx, q, org, id)
		return err
	})
	return created, err
}

// UpdateRole replaces a custom role's name and permissions at the expected version, revokes its members' sessions, and rolls back when no active administrator would remain.
func (s *IdentityStore) UpdateRole(ctx context.Context, org identity.OrganizationID, delegation identity.Delegation, role identity.RoleID, expectedVersion int64, definition identity.RoleDefinition, event audit.Event) (identity.Role, error) {
	var updated identity.Role
	err := s.withAdministrationTx(ctx, org, func(q *db.Queries, orgID pgtype.UUID, at time.Time) error {
		roleID, err := stringToUUID(string(role))
		if err != nil {
			return identity.ErrRoleNotFound
		}
		stored, err := getRole(ctx, q, org, role)
		if errors.Is(err, identity.ErrRoleNotFound) {
			return explainRefusedRoleChange(ctx, q, orgID, roleID, expectedVersion)
		}
		if err != nil {
			return err
		}
		if stored.IsSystem {
			return identity.ErrSystemRoleImmutable
		}
		if err := reauthorizeDelegation(ctx, q, orgID, delegation, stored.Permissions, definition.Permissions); err != nil {
			return err
		}
		changed, err := q.UpdateCustomRole(ctx, db.UpdateCustomRoleParams{Name: definition.Name, OrganizationID: orgID, RoleID: roleID, ExpectedVersion: expectedVersion})
		if err != nil {
			return onUniqueViolation(err, rolesOrgNameIndex, identity.ErrRoleNameTaken)
		}
		if changed == 0 {
			return explainRefusedRoleChange(ctx, q, orgID, roleID, expectedVersion)
		}
		permissionKeys := permissionStrings(definition.Permissions)
		if err := q.SoftDeleteRemovedRolePermissions(ctx, db.SoftDeleteRemovedRolePermissionsParams{At: timeToTS(at), RoleID: roleID, PermissionKeys: permissionKeys}); err != nil {
			return err
		}
		if err := q.GrantRolePermissions(ctx, db.GrantRolePermissionsParams{RoleID: roleID, PermissionKeys: permissionKeys}); err != nil {
			return err
		}
		revoked, err := q.RevokeRoleMemberSessionsAt(ctx, db.RevokeRoleMemberSessionsAtParams{At: timeToTS(at), OrganizationID: orgID, RoleID: roleID})
		if err != nil {
			return err
		}
		if err := requireActiveAdministrator(ctx, q, orgID); err != nil {
			return err
		}
		if err := commitAdministrationEvent(ctx, q, event, org, at, string(role), map[string]any{"revoked_sessions": revoked}); err != nil {
			return err
		}
		updated, err = getRole(ctx, q, org, role)
		return err
	})
	return updated, err
}

// DeleteRole soft-deletes an unassigned custom role at the expected version with the audit event.
func (s *IdentityStore) DeleteRole(ctx context.Context, org identity.OrganizationID, delegation identity.Delegation, role identity.RoleID, expectedVersion int64, event audit.Event) error {
	return s.withAdministrationTx(ctx, org, func(q *db.Queries, orgID pgtype.UUID, at time.Time) error {
		roleID, err := stringToUUID(string(role))
		if err != nil {
			return identity.ErrRoleNotFound
		}
		if err := reauthorizeDelegation(ctx, q, orgID, delegation); err != nil {
			return err
		}
		deleted, err := q.SoftDeleteCustomRole(ctx, db.SoftDeleteCustomRoleParams{At: timeToTS(at), OrganizationID: orgID, RoleID: roleID, ExpectedVersion: expectedVersion})
		if err != nil {
			return err
		}
		if deleted == 0 {
			return explainRefusedRoleChange(ctx, q, orgID, roleID, expectedVersion)
		}
		return commitAdministrationEvent(ctx, q, event, org, at, string(role), nil)
	})
}

// explainRefusedRoleChange maps a conditional role update or delete that changed no row to its domain refusal.
func explainRefusedRoleChange(ctx context.Context, q *db.Queries, orgID, roleID pgtype.UUID, expectedVersion int64) error {
	state, err := q.GetRoleState(ctx, db.GetRoleStateParams{OrganizationID: orgID, RoleID: roleID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return identity.ErrRoleNotFound
	case err != nil:
		return err
	case state.Deleted:
		return identity.ErrRoleNotFound
	case state.IsSystem:
		return identity.ErrSystemRoleImmutable
	case state.Version != expectedVersion:
		return identity.ErrRoleConflict
	case state.Assigned:
		return identity.ErrRoleInUse
	default:
		return identity.ErrRoleConflict
	}
}
