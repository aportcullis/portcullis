-- 0011_connection_policies: per-connection execution policy as immutable
-- versioned snapshots (PRD §4.3/§12.1, ADR-0015) + the environment/description
-- descriptor fields.
--
-- connection_policy_versions is APPEND-ONLY: access requests will pin a
-- version inside their approval payload, so a pinned snapshot must never
-- change — updates insert version N+1 and move the pointer on connections.
-- connections.current_policy_version has NO default on purpose: the deferred
-- composite FK below validates the pointer at commit, so a code path that
-- forgets the policy row fails its transaction instead of silently pointing
-- at nothing.

-- Descriptor additions (D8-0c): what the connection points at (the UI makes
-- production unmistakable) and free-text operator context. Plaintext
-- descriptor fields, never part of the credential envelope.
alter table public.connections
    add column if not exists environment text not null default 'development',
    add column if not exists description text not null default '';

do $$
begin
    if not exists (
        select 1 from pg_constraint
        where conname = 'connections_environment_valid'
          and conrelid = 'public.connections'::regclass
          and contype = 'c'
    ) then
        alter table public.connections
            add constraint connections_environment_valid check (environment in ('development', 'production'));
    end if;
    if not exists (
        select 1 from pg_constraint
        where conname = 'connections_description_length'
          and conrelid = 'public.connections'::regclass
          and contype = 'c'
    ) then
        alter table public.connections
            add constraint connections_description_length check (char_length(description) <= 500);
    end if;
end
$$;

-- One immutable policy version. Explicit columns (no jsonb) so the bounds live
-- in CHECK constraints (data.md); per-class approvals but ONE limit set per
-- version (ADR-0015 resolves PRD §6's ambiguity). required_approvals is kept
-- even while a class is disallowed so re-enabling does not reset the quorum.
create table if not exists connection_policy_versions (
    connection_id            uuid   not null,
    organization_id          uuid   not null,
    version                  bigint not null check (version > 0),
    read_allowed             boolean not null,
    write_allowed            boolean not null,
    ddl_allowed              boolean not null,
    read_required_approvals  int not null check (read_required_approvals between 0 and 100),
    write_required_approvals int not null check (write_required_approvals between 0 and 100),
    ddl_required_approvals   int not null check (ddl_required_approvals between 0 and 100),
    -- Limits mirror PRD §8.2 (timeout ≤ 5m, rows ≤ 10k) and ADR-0011 (a result
    -- must fit the 64 MiB per-user quota).
    query_timeout_seconds    int    not null check (query_timeout_seconds between 1 and 300),
    max_rows                 int    not null check (max_rows between 1 and 10000),
    max_result_bytes         bigint not null check (max_result_bytes between 4096 and 67108864),
    created_by               uuid not null references users (id) on delete restrict,
    created_at               timestamptz not null default now(),
    primary key (connection_id, version),
    -- Org-pinned composite FK (the 0007 unique (id, organization_id) exists
    -- precisely for this): a policy row can never cross organizations.
    foreign key (connection_id, organization_id)
        references connections (id, organization_id) on delete restrict
);

-- FK column indexes (data.md: every lookup and foreign key). connection_id is
-- covered by the PK prefix; organization_id gets its own for org-scoped scans
-- and the composite-FK integrity checks.
create index if not exists connection_policy_versions_created_by_idx
    on connection_policy_versions (created_by);
create index if not exists connection_policy_versions_org_idx
    on connection_policy_versions (organization_id);

-- SENSITIVE / append-only (data.md review gate): pinned approval evidence.
-- The 0003 default privileges hand the runtime SELECT/INSERT/UPDATE (0010
-- already stripped DELETE); revoke UPDATE so the runtime is structurally
-- unable to rewrite a snapshot. privcheck's tablePolicies entry mirrors this
-- (SELECT/INSERT required, UPDATE forbidden) so a regressed grant fails boot.
do $$
declare
    rr text := coalesce(nullif(current_setting('portcullis.runtime_role', true), ''), 'portcullis_runtime');
begin
    if exists (select 1 from pg_roles where rolname = rr) then
        execute format('revoke update on public.connection_policy_versions from %I', rr);
    end if;
end
$$;

-- Pointer to the current version + v1 default backfill for EVERY existing
-- connection, archived included: the NOT NULL invariant carries no carve-out,
-- the future restore flow finds a policy in place, and history joins never hit
-- NULL (ADR-0015). Backfilled rows carry the connection's creator; they are
-- the PRD-mandated default state (read-only, 1 approval), not an admin action,
-- so no audit event is written for them. The literal defaults below mirror
-- connection.DefaultPolicy() (domain) — keep the two in sync.
alter table public.connections
    add column if not exists current_policy_version bigint;

insert into connection_policy_versions (
    connection_id, organization_id, version,
    read_allowed, write_allowed, ddl_allowed,
    read_required_approvals, write_required_approvals, ddl_required_approvals,
    query_timeout_seconds, max_rows, max_result_bytes,
    created_by
)
select c.id, c.organization_id, 1, true, false, false, 1, 1, 1, 30, 10000, 16777216, c.created_by
from public.connections c
where not exists (
    select 1 from connection_policy_versions p where p.connection_id = c.id and p.version = 1
);

update public.connections set current_policy_version = 1 where current_policy_version is null;

alter table public.connections alter column current_policy_version set not null;

do $$
begin
    if not exists (
        select 1 from pg_constraint
        where conname = 'connections_current_policy_version_positive'
          and conrelid = 'public.connections'::regclass
          and contype = 'c'
    ) then
        alter table public.connections
            add constraint connections_current_policy_version_positive check (current_policy_version > 0);
    end if;
    -- The connection↔policy reference is circular inside the create
    -- transaction; DEFERRABLE INITIALLY DEFERRED validates it at COMMIT
    -- (web-verified: REFERENCES is deferrable, deferred = checked at commit),
    -- so insert order does not matter and a connection can never commit
    -- without its policy row.
    if not exists (
        select 1 from pg_constraint
        where conname = 'connections_current_policy_fk'
          and conrelid = 'public.connections'::regclass
          and contype = 'f'
    ) then
        alter table public.connections
            add constraint connections_current_policy_fk
            foreign key (id, current_policy_version)
            references connection_policy_versions (connection_id, version)
            deferrable initially deferred;
    end if;
end
$$;
