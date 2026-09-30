-- Config version changes only with target or credential replacement; descriptor renames do not invalidate approvals (ADR-0014/0018).
alter table public.connections
    add column if not exists config_version bigint not null default 1;

-- Positive like `version` (ADR-0014): the schema, not only the application. Constraint names are NOT globally unique (pg_constraint: "not necessarily unique!"), so the existence check pins the relation and the constraint type.
do $$
begin
    if not exists (
        select 1 from pg_constraint
        where conname = 'connections_config_version_positive'
          and conrelid = 'public.connections'::regclass
          and contype = 'c'
    ) then
        alter table public.connections
            add constraint connections_config_version_positive check (config_version > 0);
    end if;
end $$;
