-- name: ListPermissionKeys :many
select key from public.permissions order by key;

-- name: BootstrapRoleID :one
select id from public.roles
where organization_id = $1 and is_bootstrap_default and deleted_at is null
limit 1;

-- name: RoleNameForUser :one
-- Legacy display metadata only; active sessions display separate caller-owned Binding summaries.
select coalesce(string_agg(distinct role.name, ', ' order by role.name), '')::text
from public.role_bindings binding
join public.role_binding_versions version on version.binding_id=binding.id and version.organization_id=binding.organization_id and version.version=binding.current_version
join public.roles role on role.id=version.role_id and role.organization_id=version.organization_id and role.deleted_at is null
where binding.organization_id= @organization_id and binding.user_id= @user_id and version.revoked_at is null;

-- name: PermissionsForUser :many
-- Compatibility hints from live unconditional organization Bindings only; never flatten scoped or conditional grants.
select distinct permission.permission_key
from public.organization_memberships member
join public.users account on account.id=member.user_id and account.status='active'
join public.role_bindings binding on binding.organization_id=member.organization_id and binding.user_id=member.user_id
join public.role_binding_versions version on version.binding_id=binding.id and version.organization_id=binding.organization_id and version.version=binding.current_version
join public.roles role on role.id=version.role_id and role.organization_id=version.organization_id and role.deleted_at is null
join public.role_permissions permission on permission.role_id=role.id and permission.deleted_at is null
where member.organization_id= @organization_id and member.user_id= @user_id
  and version.revoked_at is null and version.scope_type='organization' and version.condition_expression is null
order by permission.permission_key;
