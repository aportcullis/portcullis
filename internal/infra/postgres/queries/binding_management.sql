-- name: LockBindingOrganization :exec
-- Share the administration lock class with policy writers; governance adopts it at atomic activation.
select pg_catalog.pg_advisory_xact_lock(sqlc.arg(lock_class)::integer, sqlc.arg(lock_object)::integer);

-- name: LockBindingRevision :one
select revision from public.organization_authorization_revisions
where organization_id = sqlc.arg(organization_id)::uuid for update;

-- name: LockBindingMember :one
select account.status from public.organization_memberships member
join public.users account on account.id = member.user_id
where member.organization_id = sqlc.arg(organization_id)::uuid and member.user_id = sqlc.arg(user_id)::uuid
for key share of account, member;

-- name: LockBindingRole :one
select id from public.roles where organization_id = sqlc.arg(organization_id)::uuid
  and id = sqlc.arg(role_id)::uuid and deleted_at is null for key share;

-- name: GetBindingPolicyRole :one
-- Reusable definitions have no dependency on the legacy membership role assignment.
select role.id, role.name, role.is_system, role.version,
       coalesce(array_agg(permission.permission_key order by permission.permission_key)
         filter (where permission.permission_key is not null), '{}')::text[] as permissions
from public.roles role
left join public.role_permissions permission on permission.role_id = role.id and permission.deleted_at is null
where role.organization_id = sqlc.arg(organization_id)::uuid and role.id = sqlc.arg(role_id)::uuid
  and role.deleted_at is null
group by role.id;

-- name: GetManagedBinding :one
select version.* from public.role_bindings binding
join public.role_binding_versions version on version.binding_id = binding.id
  and version.organization_id = binding.organization_id and version.user_id = binding.user_id
  and version.version = binding.current_version
where binding.organization_id = sqlc.arg(organization_id)::uuid and binding.id = sqlc.arg(binding_id)::uuid
for update of binding;

-- name: GetUnconditionalBindingPermissions :many
-- Authority for delegation never evaluates or flattens conditional or connection grants.
select distinct permission.permission_key from public.role_bindings binding
join public.role_binding_versions version on version.binding_id = binding.id
  and version.organization_id = binding.organization_id and version.user_id = binding.user_id and version.version = binding.current_version
join public.roles role on role.id = version.role_id and role.organization_id = version.organization_id and role.deleted_at is null
join public.role_permissions permission on permission.role_id = role.id and permission.deleted_at is null
where binding.organization_id = sqlc.arg(organization_id)::uuid and binding.user_id = sqlc.arg(user_id)::uuid
  and version.revoked_at is null and version.scope_type = 'organization' and version.connection_id is null
  and version.condition_expression is null and version.condition_title is null and version.condition_description is null
  and version.condition_language_version is null and version.condition_environment_version is null
order by permission.permission_key;

-- name: LockBindingScopeConnection :one
select id from public.connections where organization_id = sqlc.arg(organization_id)::uuid
  and id = sqlc.arg(connection_id)::uuid for key share;

-- name: InsertManagedBindingHead :one
insert into public.role_bindings (organization_id, user_id, current_version, created_at)
values (sqlc.arg(organization_id)::uuid, sqlc.arg(user_id)::uuid, 1, sqlc.arg(at)::timestamptz)
returning id;

-- name: InsertManagedBindingVersion :exec
insert into public.role_binding_versions
(binding_id, organization_id, user_id, version, role_id, scope_type, connection_id,
 condition_title, condition_description, condition_expression, condition_language_version, condition_environment_version, created_at, revoked_at)
values (sqlc.arg(binding_id)::uuid, sqlc.arg(organization_id)::uuid, sqlc.arg(user_id)::uuid,
 sqlc.arg(version)::bigint, sqlc.arg(role_id)::uuid, sqlc.arg(scope_type)::text, sqlc.narg(connection_id)::uuid,
 sqlc.narg(condition_title)::text, sqlc.narg(condition_description)::text, sqlc.narg(condition_expression)::text,
 sqlc.narg(condition_language_version)::text, sqlc.narg(condition_environment_version)::text,
 sqlc.arg(at)::timestamptz, sqlc.narg(revoked_at)::timestamptz);

-- name: AdvanceManagedBindingHead :execrows
update public.role_bindings set current_version = sqlc.arg(next_version)::bigint
where organization_id = sqlc.arg(organization_id)::uuid and id = sqlc.arg(binding_id)::uuid
  and user_id = sqlc.arg(user_id)::uuid and current_version = sqlc.arg(expected_version)::bigint;

-- name: HasBindingRecoveryAdministrator :one
-- Every required key may come from a different unconditional organization binding of the same active member.
select exists (
 select 1 from public.organization_memberships member
 join public.users account on account.id = member.user_id and account.status = 'active'
 where member.organization_id = sqlc.arg(organization_id)::uuid
 and not exists (
  select required.permission_key from pg_catalog.unnest(sqlc.arg(required_permissions)::text[]) as required(permission_key)
  where not exists (
   select 1 from public.role_bindings binding
   join public.role_binding_versions version on version.binding_id = binding.id
    and version.organization_id = binding.organization_id and version.user_id = binding.user_id and version.version = binding.current_version
   join public.roles role on role.id = version.role_id and role.organization_id = version.organization_id and role.deleted_at is null
   join public.role_permissions permission on permission.role_id = role.id and permission.deleted_at is null
   where binding.organization_id = member.organization_id and binding.user_id = member.user_id
    and permission.permission_key = required.permission_key
    and version.revoked_at is null and version.scope_type = 'organization' and version.connection_id is null
  and version.condition_expression is null and version.condition_title is null and version.condition_description is null
  and version.condition_language_version is null and version.condition_environment_version is null
  )
 )
)::boolean;

-- name: AdvanceBindingPolicyRevision :one
update public.organization_authorization_revisions set revision = revision + 1
where organization_id = sqlc.arg(organization_id)::uuid and revision = sqlc.arg(expected_revision)::bigint
returning revision;

-- name: LockBindingSubjectSession :exec
-- Coordinate affected-session revocation with the existing per-user login rotation lock.
select pg_catalog.pg_advisory_xact_lock(sqlc.arg(lock_class)::integer, sqlc.arg(lock_object)::integer);

-- name: LockBindingSessionRows :exec
-- Lock existing affected rows before the mutation clock observation; session rotation cannot add rows while its advisory lock is held.
select id from public.sessions where user_id = sqlc.arg(user_id)::uuid and revoked_at is null
order by id for update;
