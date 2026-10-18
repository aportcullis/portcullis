-- Prepare scoped binding persistence without activating a second grant source (ADR-0062/0064).
-- Backfill, catalog additions, runtime writes and authorization cutover belong to one later activation; no legacy assignment is copied here.

-- The head identifies a member and points to one immutable snapshot; revocation is another snapshot, not row deletion.
create table if not exists public.role_bindings (
    id               uuid primary key default gen_random_uuid(),
    organization_id  uuid not null,
    user_id          uuid not null,
    current_version  bigint not null check (current_version > 0),
    created_at       timestamptz not null,
    unique (id, organization_id, user_id),
    constraint role_bindings_member_in_org foreign key (organization_id, user_id)
        references public.organization_memberships (organization_id, user_id) on delete restrict
);
create index if not exists role_bindings_org_user_idx
    on public.role_bindings (organization_id, user_id, id);
create index if not exists role_bindings_user_idx on public.role_bindings (user_id);

-- Append-only authorization evidence: each snapshot owns its optional condition source and language/environment identity.
-- These rows retain the role, resource and condition that applied even after the head advances or is revoked.
create table if not exists public.role_binding_versions (
    binding_id                     uuid not null,
    organization_id                uuid not null,
    user_id                        uuid not null,
    version                        bigint not null check (version > 0),
    role_id                        uuid not null,
    scope_type                     text not null,
    connection_id                  uuid,
    condition_title                text,
    condition_description          text,
    condition_expression           text,
    condition_language_version     text,
    condition_environment_version  text,
    created_at                     timestamptz not null,
    revoked_at                     timestamptz,
    primary key (binding_id, version),
    unique (binding_id, organization_id, version),
    constraint role_binding_versions_binding_in_org foreign key (binding_id, organization_id, user_id)
        references public.role_bindings (id, organization_id, user_id) on delete restrict,
    constraint role_binding_versions_role_in_org foreign key (role_id, organization_id)
        references public.roles (id, organization_id) on delete restrict,
    constraint role_binding_versions_connection_in_org foreign key (connection_id, organization_id)
        references public.connections (id, organization_id) on delete restrict,
    constraint role_binding_versions_scope_shape check (
        (scope_type = 'organization' and connection_id is null)
        or (scope_type = 'connection' and connection_id is not null)),
    constraint role_binding_versions_condition_shape check (
        (condition_title is null and condition_description is null and condition_expression is null
            and condition_language_version is null and condition_environment_version is null)
        or (condition_title is not null and btrim(condition_title) <> ''
            and condition_expression is not null and btrim(condition_expression) <> ''
            and condition_language_version is not null and btrim(condition_language_version) <> ''
            and condition_environment_version is not null and btrim(condition_environment_version) <> '')),
    constraint role_binding_versions_revocation_time check (revoked_at is null or revoked_at >= created_at)
);
create index if not exists role_binding_versions_org_idx
    on public.role_binding_versions (organization_id, binding_id, version);
create index if not exists role_binding_versions_role_idx
    on public.role_binding_versions (role_id, organization_id);
create index if not exists role_binding_versions_connection_idx
    on public.role_binding_versions (connection_id, organization_id);

-- Both rows are inserted atomically, like connection/policy snapshots; no head may commit without its exact organization-pinned version.
do $$
begin
    if not exists (
        select 1 from pg_catalog.pg_constraint
        where conname = 'role_bindings_current_version_in_org'
          and conrelid = 'public.role_bindings'::regclass and contype = 'f'
    ) then
        alter table public.role_bindings
            add constraint role_bindings_current_version_in_org
            foreign key (id, organization_id, current_version)
            references public.role_binding_versions (binding_id, organization_id, version)
            on delete restrict deferrable initially deferred;
    end if;
end
$$;

-- Mutable coordination state once activated; deliberately empty while legacy authorization remains authoritative.
create table if not exists public.organization_authorization_revisions (
    organization_id  uuid primary key references public.organizations (id) on delete restrict,
    revision         bigint not null check (revision > 0)
);

-- All three tables are read-only to runtime during preparation so existing code cannot accumulate ignored or stale grants.
-- Activation may grant head/revision writes and version INSERT; version UPDATE/DELETE must remain forbidden permanently.
do $$
declare
    runtime_role text := coalesce(nullif(current_setting('portcullis.runtime_role', true), ''), 'portcullis_runtime');
begin
    execute format('revoke insert, update, delete, truncate, trigger, references, maintain on public.role_bindings, public.role_binding_versions, public.organization_authorization_revisions from %I', runtime_role);
end
$$;
