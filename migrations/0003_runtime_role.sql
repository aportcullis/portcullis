-- Runtime permission boundary (ADR-0009; PRD "runtime DB role"): the app
-- connects as a member of portcullis_runtime, which can operate the tables but
-- can only ever APPEND to audit_events (INSERT/SELECT — no UPDATE/DELETE), and,
-- not being the table owner, cannot ALTER tables or disable the append-only
-- triggers. Migrations keep running as the owner; deployments create a LOGIN
-- user and GRANT portcullis_runtime TO it (compose does this via an init script).

-- Roles are cluster-wide while migrations run per-database: guard existence and
-- tolerate a concurrent creation from another database's migration.
do $$
begin
    if not exists (select 1 from pg_roles where rolname = 'portcullis_runtime') then
        begin
            create role portcullis_runtime nologin;
        exception when duplicate_object then
            null; -- created concurrently by another database's migration
        end;
    end if;
end
$$;

-- Connection isolation: make CONNECT explicit instead of PUBLIC's implicit
-- grant, so a role from another install on a shared cluster cannot even
-- connect to this database (the owner keeps CONNECT implicitly).
do $$
begin
    execute format('revoke connect on database %I from public', current_database());
    execute format('grant connect on database %I to portcullis_runtime', current_database());
end
$$;

grant usage on schema public to portcullis_runtime;
grant select, insert, update, delete on all tables in schema public to portcullis_runtime;

-- Audit is append-only for the runtime: read and append, never mutate.
revoke update, delete on audit_events from portcullis_runtime;

-- Migration history is owner-only: with any access the runtime could delete
-- applied records (forcing re-runs) or pre-insert future versions (skipping
-- security migrations).
revoke all on schema_migrations from portcullis_runtime;

-- Tables added by future migrations (run by the owner) get DML automatically;
-- a migration adding a sensitive table must REVOKE in that same migration
-- (docs/conventions/data.md).
alter default privileges in schema public
    grant select, insert, update, delete on tables to portcullis_runtime;

-- Dev convenience: compose's init script creates the portcullis_app login user
-- before first boot; give it the runtime role when present. The name comparison
-- guards the direct-login shape (PORTCULLIS_RUNTIME_ROLE=portcullis_app): after
-- substitution it collapses to 'x' <> 'x', skipping the forbidden self-grant.
do $$
begin
    if exists (select 1 from pg_roles where rolname = 'portcullis_app')
       and 'portcullis_runtime' <> 'portcullis_app' then
        grant portcullis_runtime to portcullis_app;
    end if;
end
$$;
