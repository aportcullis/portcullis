-- 0009_connection_version_and_runtime_grant: a bigint mutation token replaces
-- timestamp optimistic locking (ADR-0014), and repeats 0003's best-effort
-- runtime-role membership handling for already-migrated installations.
alter table public.connections
    add column if not exists version bigint not null default 1;

-- The mutation token is a positive bigint (ADR-0014); enforce it in the schema,
-- not just the application. Idempotent so a re-run (or an install that already
-- ran an earlier form of 0009) adds it exactly once. Constraint names are NOT
-- globally unique (pg_constraint: "not necessarily unique!"), so the existence
-- check must pin the relation and constraint type or a same-named constraint on
-- another table would silently skip this one.
do $$
begin
    if not exists (
        select 1 from pg_constraint
        where conname = 'connections_version_positive'
          and conrelid = 'public.connections'::regclass
          and contype = 'c'
    ) then
        alter table public.connections
            add constraint connections_version_positive check (version > 0);
    end if;
end
$$;

do $$
declare
    rr text := coalesce(nullif(current_setting('portcullis.runtime_role', true), ''), 'portcullis_runtime');
begin
    -- 0003 may already be deployed, so repeat only its best-effort development
    -- convenience grant here instead of changing that released migration.
    if rr = 'portcullis_runtime'
       and exists (select 1 from pg_roles where rolname = 'portcullis_app') then
        begin
            grant portcullis_runtime to portcullis_app;
        exception when insufficient_privilege then
            null;
        end;
    end if;
end
$$;
