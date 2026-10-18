-- name: LockSharedPolicyOrganization :exec
select pg_catalog.pg_advisory_xact_lock_shared(sqlc.arg(lock_class)::integer,sqlc.arg(lock_object)::integer);

-- name: IsBindingAuthorizationActivated :one
select exists(select 1 from pg_catalog.pg_constraint where conrelid='public.organization_memberships'::regclass and conname='membership_legacy_role_retired' and convalidated)
 and not exists(select 1 from public.organization_memberships where role_id is not null)
 and not exists(select 1 from public.organizations organization left join public.organization_authorization_revisions revision on revision.organization_id=organization.id where revision.revision is null or revision.revision<=0);

-- name: InsertBootstrapPasswordAuth :exec
insert into public.auth_methods(user_id,type,secret,created_at,updated_at)
values(sqlc.arg(user_id)::uuid,'password',sqlc.arg(secret)::text,sqlc.arg(at)::timestamptz,sqlc.arg(at)::timestamptz);

-- name: ListAuthorizationConnectionCandidates :many
select * from public.connections
where organization_id=sqlc.arg(organization_id)::uuid
 and (sqlc.arg(include_archived)::boolean or archived_at is null)
order by created_at desc,id desc limit sqlc.arg(candidate_limit)::integer;

-- name: ResolveExecutionResultIdentity :one
select execution.result_id from public.query_executions execution
join public.access_requests request on request.id=execution.request_id and request.organization_id=execution.organization_id
where execution.organization_id=sqlc.arg(organization_id)::uuid and execution.request_id=sqlc.arg(request_id)::uuid
 and execution.result_id is not null;

-- name: HasBindingPermissionCandidate :one
-- This is scan admission only; every returned resource must pass the actual evaluator.
select exists(
 select 1 from public.organization_memberships member join public.users account on account.id=member.user_id and account.status='active'
 join public.organization_authorization_revisions revision on revision.organization_id=member.organization_id and revision.revision>0
 join public.role_bindings binding on binding.user_id=member.user_id and binding.organization_id=member.organization_id
 join public.role_binding_versions version on version.binding_id=binding.id and version.organization_id=binding.organization_id and version.version=binding.current_version
 join public.roles role on role.id=version.role_id and role.organization_id=version.organization_id and role.deleted_at is null
 join public.role_permissions permission on permission.role_id=role.id and permission.deleted_at is null
 where member.organization_id=sqlc.arg(organization_id)::uuid and member.user_id=sqlc.arg(user_id)::uuid
 and permission.permission_key=sqlc.arg(permission_key)::text and version.revoked_at is null
 and (version.scope_type='organization' or (sqlc.arg(allow_connection)::boolean and version.scope_type='connection'))
)::boolean;

-- name: ListPolicyApprovalCandidates :many
select approval.approver_id from public.approvals approval
where approval.organization_id=sqlc.arg(organization_id)::uuid and approval.request_id=sqlc.arg(request_id)::uuid
 and approval.decision='approved' and approval.approver_id<>sqlc.arg(requester_id)::uuid
order by approval.approver_id limit sqlc.arg(candidate_limit)::integer;

-- name: ListSessionBindings :many
select binding.id,version.role_id,role.name as role_name,version.scope_type,version.connection_id,
 (version.condition_expression is not null)::boolean as has_condition
from public.role_bindings binding
join public.role_binding_versions version on version.binding_id=binding.id and version.organization_id=binding.organization_id and version.version=binding.current_version
join public.roles role on role.id=version.role_id and role.organization_id=version.organization_id and role.deleted_at is null
where binding.organization_id=sqlc.arg(organization_id)::uuid and binding.user_id=sqlc.arg(user_id)::uuid and version.revoked_at is null
order by binding.id limit sqlc.arg(candidate_limit)::integer;

-- name: ListCapabilityTargets :many
-- Identity-only bounded scan; every candidate still passes resource authorization before becoming a hint.
with targets as (
select 'organization'::text as kind,organization.id from public.organizations organization where organization.id=sqlc.arg(organization_id)::uuid
union all select 'connection'::text,connection.id from public.connections connection where connection.organization_id=sqlc.arg(organization_id)::uuid
union all select 'policy'::text,connection.id from public.connections connection where connection.organization_id=sqlc.arg(organization_id)::uuid
union all select 'request'::text,request.id from public.access_requests request where request.organization_id=sqlc.arg(organization_id)::uuid
union all select 'user'::text,member.user_id from public.organization_memberships member where member.organization_id=sqlc.arg(organization_id)::uuid
union all select 'role'::text,role.id from public.roles role where role.organization_id=sqlc.arg(organization_id)::uuid and role.deleted_at is null
union all select 'binding'::text,binding.id from public.role_bindings binding where binding.organization_id=sqlc.arg(organization_id)::uuid
union all select 'audit'::text,event.id from public.audit_events event where event.organization_id=sqlc.arg(organization_id)::uuid
union all select 'execution'::text,execution.request_id from public.query_executions execution where execution.organization_id=sqlc.arg(organization_id)::uuid
union all select 'result'::text,result.id from result_cache.result_sets result where result.organization_id=sqlc.arg(organization_id)::uuid and result.expires_at>sqlc.arg(evaluated_at)::timestamptz
)
select kind,id from targets where kind=any(sqlc.arg(resource_types)::text[])
order by kind,id limit sqlc.arg(candidate_limit)::integer;

-- name: ListSessionPermissionCandidates :many
-- Potential keys narrow advisory scans; they do not authorize any resource.
select distinct permission.permission_key
from public.role_bindings binding
join public.role_binding_versions version on version.binding_id=binding.id and version.organization_id=binding.organization_id and version.version=binding.current_version
join public.roles role on role.id=version.role_id and role.organization_id=version.organization_id and role.deleted_at is null
join public.role_permissions permission on permission.role_id=role.id and permission.deleted_at is null
where binding.organization_id=sqlc.arg(organization_id)::uuid and binding.user_id=sqlc.arg(user_id)::uuid and version.revoked_at is null
order by permission.permission_key;
