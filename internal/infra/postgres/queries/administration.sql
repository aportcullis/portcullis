-- name: ListMembers :many
-- Users seen through their membership in one organization (ADR-0004/0053), with the single assigned role and whether a password credential exists.
select u.id, u.email, u.display_name, u.status, u.created_at, m.role_id, r.name as role_name,
       exists (select 1 from public.auth_methods am where am.user_id = u.id and am.type = 'password')::boolean as has_password
from public.organization_memberships m
join public.users u on u.id = m.user_id
join public.roles r on r.id = m.role_id and r.organization_id = m.organization_id
where m.organization_id = @organization_id
order by lower(u.email), u.id;

-- name: GetMember :one
select u.id, u.email, u.display_name, u.status, u.created_at, m.role_id, r.name as role_name,
       exists (select 1 from public.auth_methods am where am.user_id = u.id and am.type = 'password')::boolean as has_password
from public.organization_memberships m
join public.users u on u.id = m.user_id
join public.roles r on r.id = m.role_id and r.organization_id = m.organization_id
where m.organization_id = @organization_id and m.user_id = @user_id;

-- name: LockMember :one
-- Lock the account and its membership for a status, role or setup-link change; a user without a membership in the organization is not found.
select u.status,
       exists (select 1 from public.auth_methods am where am.user_id = u.id and am.type = 'password')::boolean as has_password
from public.organization_memberships m
join public.users u on u.id = m.user_id
where m.organization_id = @organization_id and m.user_id = @user_id
for update of u, m;

-- name: SetUserStatus :exec
update public.users set status = @status where id = @user_id;

-- name: SetMembershipRole :exec
update public.organization_memberships set role_id = @role_id
where organization_id = @organization_id and user_id = @user_id;

-- name: RevokeUserSessionsAt :execrows
-- Revoke every active session of a user at the observed instant of a privilege change (ADR-0006/0053).
update public.sessions set revoked_at = @at::timestamptz
where user_id = @user_id and revoked_at is null;

-- name: RevokeRoleMemberSessionsAt :execrows
-- Revoke every active session of the organization's members holding a role whose permissions changed.
update public.sessions set revoked_at = @at::timestamptz
where revoked_at is null
  and user_id in (
      select m.user_id from public.organization_memberships m
      where m.organization_id = @organization_id and m.role_id = @role_id
  );

-- name: CountActiveAdministrators :one
-- Active members whose live role holds every administrator permission key (ADR-0008); the keys come from the domain rule.
select count(*)
from public.organization_memberships m
join public.users u on u.id = m.user_id and u.status = 'active'
join public.roles r on r.id = m.role_id and r.organization_id = m.organization_id and r.deleted_at is null
where m.organization_id = @organization_id
  and (select count(distinct rp.permission_key)
       from public.role_permissions rp
       where rp.role_id = m.role_id and rp.deleted_at is null and rp.permission_key = any(@administrator_permissions::text[]))
      = cardinality(@administrator_permissions::text[]);

-- name: InsertPasswordSetupToken :one
-- The expiry is measured from the observed instant on the database clock.
insert into public.password_setup_tokens (organization_id, user_id, token_hash, expires_at, created_at)
select @organization_id, @user_id, @token_hash, stamped.at + stamped.validity, stamped.at
from (select @at::timestamptz as at, make_interval(secs => @validity_seconds::float8) as validity) as stamped
returning expires_at;

-- name: RevokeOpenPasswordSetups :exec
update public.password_setup_tokens set revoked_at = @at::timestamptz
where user_id = @user_id and consumed_at is null and revoked_at is null;

-- name: GetRoleWithPermissions :one
select r.id, r.name, r.is_system, r.version,
       coalesce(array_agg(rp.permission_key order by rp.permission_key) filter (where rp.permission_key is not null), '{}')::text[] as permissions,
       (select count(*) from public.organization_memberships m where m.role_id = r.id and m.organization_id = r.organization_id) as member_count
from public.roles r
left join public.role_permissions rp on rp.role_id = r.id and rp.deleted_at is null
where r.organization_id = @organization_id and r.id = @role_id and r.deleted_at is null
group by r.id;

-- name: ListRolesWithPermissions :many
select r.id, r.name, r.is_system, r.version,
       coalesce(array_agg(rp.permission_key order by rp.permission_key) filter (where rp.permission_key is not null), '{}')::text[] as permissions,
       (select count(*) from public.organization_memberships m where m.role_id = r.id and m.organization_id = r.organization_id) as member_count
from public.roles r
left join public.role_permissions rp on rp.role_id = r.id and rp.deleted_at is null
where r.organization_id = @organization_id and r.deleted_at is null
group by r.id
order by r.is_system desc, lower(r.name), r.id;

-- name: GetRoleState :one
-- Disambiguates a refused role update or delete, including soft-deleted rows.
select r.is_system, r.version, (r.deleted_at is not null)::boolean as deleted,
       exists (select 1 from public.organization_memberships m where m.role_id = r.id and m.organization_id = r.organization_id)::boolean as assigned
from public.roles r
where r.organization_id = @organization_id and r.id = @role_id;

-- name: InsertRole :one
insert into public.roles (organization_id, name, is_system, is_bootstrap_default, version, created_at)
values (@organization_id, @name, false, false, 1, @at::timestamptz)
returning id;

-- name: GrantRolePermissions :exec
-- Insert the role's permissions, reviving any soft-deleted row of the same key instead of adding a second one (ADR-0053).
insert into public.role_permissions (role_id, permission_key)
select @role_id, unnest(@permission_keys::text[])
on conflict (role_id, permission_key) do update set deleted_at = null
where role_permissions.deleted_at is not null;

-- name: SoftDeleteRemovedRolePermissions :exec
-- Soft-delete the role's live permissions missing from the new set; rows are never erased (ADR-0053).
update public.role_permissions set deleted_at = @at::timestamptz
where role_id = @role_id and deleted_at is null
  and permission_key <> all(@permission_keys::text[]);

-- name: UpdateCustomRole :execrows
update public.roles set name = @name, version = version + 1
where organization_id = @organization_id and id = @role_id and deleted_at is null
  and not is_system and version = @expected_version;

-- name: SoftDeleteCustomRole :execrows
update public.roles set deleted_at = @at::timestamptz, version = roles.version + 1
where roles.organization_id = @organization_id and roles.id = @role_id and roles.deleted_at is null
  and not roles.is_system and roles.version = @expected_version
  and not exists (select 1 from public.organization_memberships m where m.role_id = roles.id and m.organization_id = roles.organization_id);
