-- name: ListPermissionKeys :many
select key from public.permissions order by key;

-- name: BootstrapRoleID :one
select id from public.roles
where organization_id = $1 and is_bootstrap_default and deleted_at is null
limit 1;

-- name: RoleNameForUser :one
-- The display name of the user's role within one organization — a UI label (ADR-0008: authorization decisions never consult role names). The same soft-delete join rule as PermissionsForUser applies.
select r.name
from public.organization_memberships m
join public.roles r on r.id = m.role_id and r.deleted_at is null
where m.organization_id = @organization_id and m.user_id = @user_id
limit 1;

-- name: PermissionsForUser :many
-- A user's effective permissions within one organization. Scoped by org (ADR-0004 repository contract) and joined to roles so a soft-deleted role stops granting its permissions even while a membership still references it (FKs are RESTRICT).
select distinct rp.permission_key
from public.organization_memberships m
join public.roles r on r.id = m.role_id and r.deleted_at is null
join public.role_permissions rp on rp.role_id = m.role_id and rp.deleted_at is null
where m.organization_id = @organization_id and m.user_id = @user_id;
