-- name: ListPermissionKeys :many
select key from permissions order by key;

-- name: BootstrapRoleID :one
select id from roles
where organization_id = $1 and is_bootstrap_default and deleted_at is null
limit 1;

-- name: PermissionsForUser :many
-- A user's effective permissions within one organization. Scoped by org (ADR-0004
-- repository contract) and joined to roles so a soft-deleted role stops granting
-- its permissions even while a membership still references it (FKs are RESTRICT).
select distinct rp.permission_key
from organization_memberships m
join roles r on r.id = m.role_id and r.deleted_at is null
join role_permissions rp on rp.role_id = m.role_id
where m.organization_id = @organization_id and m.user_id = @user_id;
