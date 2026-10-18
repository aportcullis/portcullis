-- name: GetManagementAccount :one
select account.id,account.email,account.display_name,account.status,account.created_at,
 exists(select 1 from public.auth_methods method where method.user_id=account.id and method.type='password')::boolean as has_password
from public.organization_memberships member
join public.users account on account.id=member.user_id
where member.organization_id=sqlc.arg(organization_id)::uuid and member.user_id=sqlc.arg(user_id)::uuid;

-- name: GetManagementBinding :one
select version.* from public.role_bindings binding
join public.role_binding_versions version on version.binding_id=binding.id
 and version.organization_id=binding.organization_id and version.user_id=binding.user_id and version.version=binding.current_version
where binding.organization_id=sqlc.arg(organization_id)::uuid and binding.id=sqlc.arg(binding_id)::uuid;

-- name: ListManagementIDs :many
select member.user_id as id from public.organization_memberships member
where sqlc.arg(kind)::text='user' and member.organization_id=sqlc.arg(organization_id)::uuid
union all
select role.id from public.roles role
where sqlc.arg(kind)::text='role' and role.organization_id=sqlc.arg(organization_id)::uuid and role.deleted_at is null
union all
select binding.id from public.role_bindings binding
where sqlc.arg(kind)::text='binding' and binding.organization_id=sqlc.arg(organization_id)::uuid
order by id
limit sqlc.arg(page_limit)::integer offset sqlc.arg(page_offset)::bigint;

-- name: CountManagementRows :one
select case sqlc.arg(kind)::text
 when 'user' then (select count(*) from public.organization_memberships member where member.organization_id=sqlc.arg(organization_id)::uuid)
 when 'role' then (select count(*) from public.roles role where role.organization_id=sqlc.arg(organization_id)::uuid and role.deleted_at is null)
 when 'binding' then (select count(*) from public.role_bindings binding where binding.organization_id=sqlc.arg(organization_id)::uuid)
 else 0 end::bigint;
