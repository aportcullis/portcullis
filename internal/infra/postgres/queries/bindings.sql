-- name: GetBindingAuthorizationContext :one
-- Read membership independently of its legacy role, plus revision and explicit database time.
select (account.status = 'active')::boolean as active_member,
       coalesce(revision.revision, 0)::bigint as revision,
       clock_timestamp()::timestamptz as evaluated_at
from public.organization_memberships member
join public.users account on account.id = member.user_id
left join public.organization_authorization_revisions revision on revision.organization_id = member.organization_id
where member.organization_id = sqlc.arg(organization_id)::uuid and member.user_id = sqlc.arg(user_id)::uuid;

-- name: ResolveBindingResource :many
-- Read identifiers only; execution identity is request_id and results inherit ancestry through execution metadata.
select resource.id as resource_id, null::uuid as connection_id
from public.organizations resource
where sqlc.arg(resource_type)::text = 'organization' and resource.id = sqlc.arg(organization_id)::uuid
  and resource.id = sqlc.arg(resource_id)::uuid
union all
select resource.id as resource_id, resource.id as connection_id
from public.connections resource
where sqlc.arg(resource_type)::text = 'connection' and resource.organization_id = sqlc.arg(organization_id)::uuid
  and resource.id = sqlc.arg(resource_id)::uuid
union all
select resource.id as resource_id, resource.id as connection_id
from public.connections resource
where sqlc.arg(resource_type)::text = 'policy' and resource.organization_id = sqlc.arg(organization_id)::uuid
  and resource.id = sqlc.arg(resource_id)::uuid
union all
select resource.id as resource_id, resource.connection_id as connection_id
from public.access_requests resource
where sqlc.arg(resource_type)::text = 'request' and resource.organization_id = sqlc.arg(organization_id)::uuid
  and resource.id = sqlc.arg(resource_id)::uuid
union all
select execution.request_id as resource_id, resource.connection_id as connection_id
from public.query_executions execution join public.access_requests resource on resource.id = execution.request_id and resource.organization_id = execution.organization_id
where sqlc.arg(resource_type)::text = 'execution' and execution.organization_id = sqlc.arg(organization_id)::uuid
  and execution.request_id = sqlc.arg(resource_id)::uuid
union all
select execution.result_id as resource_id, resource.connection_id as connection_id
from public.query_executions execution join public.access_requests resource on resource.id = execution.request_id and resource.organization_id = execution.organization_id
where sqlc.arg(resource_type)::text = 'result' and execution.organization_id = sqlc.arg(organization_id)::uuid
  and execution.result_id = sqlc.arg(resource_id)::uuid
union all
select resource.user_id as resource_id, null::uuid as connection_id
from public.organization_memberships resource
where sqlc.arg(resource_type)::text = 'user' and resource.organization_id = sqlc.arg(organization_id)::uuid
  and resource.user_id = sqlc.arg(resource_id)::uuid
union all
select resource.id as resource_id, null::uuid as connection_id
from public.roles resource
where sqlc.arg(resource_type)::text = 'role' and resource.organization_id = sqlc.arg(organization_id)::uuid and resource.deleted_at is null
  and resource.id = sqlc.arg(resource_id)::uuid
union all
select resource.id as resource_id, null::uuid as connection_id
from public.role_bindings resource
where sqlc.arg(resource_type)::text = 'binding' and resource.organization_id = sqlc.arg(organization_id)::uuid
  and resource.id = sqlc.arg(resource_id)::uuid
union all
select resource.id as resource_id, null::uuid as connection_id
from public.audit_events resource
where sqlc.arg(resource_type)::text = 'audit' and resource.organization_id = sqlc.arg(organization_id)::uuid
  and resource.id = sqlc.arg(resource_id)::uuid
union all
select resource.id as resource_id, null::uuid as connection_id
from public.organizations resource
where sqlc.arg(resource_type)::text = 'settings' and resource.id = sqlc.arg(organization_id)::uuid
  and resource.id = sqlc.arg(resource_id)::uuid
limit 2;

-- name: ListResourceBindingGrants :many
-- Fetch one extra candidate to reject overflow instead of authorizing a truncated prefix.
select version.*, role.version as role_version
from public.role_bindings binding
join public.role_binding_versions version on version.binding_id = binding.id
    and version.organization_id = binding.organization_id and version.user_id = binding.user_id
    and version.version = binding.current_version
join public.roles role on role.id = version.role_id and role.organization_id = version.organization_id and role.deleted_at is null
join public.role_permissions permission on permission.role_id = role.id and permission.deleted_at is null
where binding.organization_id = sqlc.arg(organization_id)::uuid and binding.user_id = sqlc.arg(user_id)::uuid
  and version.revoked_at is null and permission.permission_key = sqlc.arg(permission_key)::text
  and (version.scope_type = 'organization' or (version.scope_type = 'connection' and version.connection_id = sqlc.narg(connection_id)::uuid))
order by binding.id
limit sqlc.arg(candidate_limit)::integer;
