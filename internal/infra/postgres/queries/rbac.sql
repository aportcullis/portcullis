-- name: ListPermissionKeys :many
select key from permissions order by key;

-- name: BootstrapRoleID :one
select id from roles
where organization_id = $1 and is_bootstrap_default and deleted_at is null
limit 1;

-- name: PermissionsForUser :many
-- Join roles so a soft-deleted role stops granting its permissions even while
-- memberships still reference it (FKs are RESTRICT, so the row lingers).
select distinct rp.permission_key
from organization_memberships m
join roles r on r.id = m.role_id and r.deleted_at is null
join role_permissions rp on rp.role_id = m.role_id
where m.user_id = $1;
