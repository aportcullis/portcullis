-- name: LockPolicyAccount :one
select account.id, account.email, account.display_name, account.status, account.created_at,
 exists (select 1 from public.auth_methods method where method.user_id=account.id and method.type='password')::boolean as has_password
from public.organization_memberships member
join public.users account on account.id=member.user_id
where member.organization_id=sqlc.arg(organization_id)::uuid and member.user_id=sqlc.arg(user_id)::uuid
for no key update of account, member;

-- name: InsertPolicyUser :one
insert into public.users (email,display_name,created_at)
values (sqlc.arg(email)::text,sqlc.arg(display_name)::text,sqlc.arg(at)::timestamptz)
returning id;

-- name: InsertIndependentMembership :exec
-- The activation schema removes the required legacy assignment; no role is inferred here.
insert into public.organization_memberships (organization_id,user_id,created_at)
values (sqlc.arg(organization_id)::uuid,sqlc.arg(user_id)::uuid,sqlc.arg(at)::timestamptz);

-- name: GetAccountBindingPermissions :many
-- Include inactive predicates and every scope because account changes affect all live grants.
select distinct permission.permission_key
from public.role_bindings binding
join public.role_binding_versions version on version.binding_id=binding.id
 and version.organization_id=binding.organization_id and version.user_id=binding.user_id and version.version=binding.current_version
join public.roles role on role.id=version.role_id and role.organization_id=version.organization_id and role.deleted_at is null
join public.role_permissions permission on permission.role_id=role.id and permission.deleted_at is null
where binding.organization_id=sqlc.arg(organization_id)::uuid and binding.user_id=sqlc.arg(user_id)::uuid
 and version.revoked_at is null
order by permission.permission_key;

-- name: GetRoleBindingSubjects :many
-- Each affected account occurs once, including disabled subjects and currently false or expired conditions.
select account.id
from public.organization_memberships member
join public.users account on account.id=member.user_id
where member.organization_id=sqlc.arg(organization_id)::uuid
 and exists (
 select 1 from public.role_bindings binding
 join public.role_binding_versions version on version.binding_id=binding.id
  and version.organization_id=binding.organization_id and version.user_id=binding.user_id and version.version=binding.current_version
 where binding.organization_id=member.organization_id and binding.user_id=member.user_id
  and version.role_id=sqlc.arg(role_id)::uuid and version.revoked_at is null
 )
order by account.id
limit sqlc.arg(subject_limit)::integer
for key share of account, member;

-- name: SoftDeletePolicyRole :execrows
update public.roles set deleted_at=sqlc.arg(at)::timestamptz, version=roles.version+1
where roles.organization_id=sqlc.arg(organization_id)::uuid and roles.id=sqlc.arg(role_id)::uuid
 and roles.deleted_at is null and not roles.is_system and roles.version=sqlc.arg(expected_version)::bigint
 and not exists (
 select 1 from public.role_bindings binding
 join public.role_binding_versions version on version.binding_id=binding.id
  and version.organization_id=binding.organization_id and version.user_id=binding.user_id and version.version=binding.current_version
 where binding.organization_id=roles.organization_id and version.role_id=roles.id and version.revoked_at is null
 );
