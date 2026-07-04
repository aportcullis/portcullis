-- Runtime permission boundary (ADR-0009; PRD "runtime DB role"): the app
-- connects as a member of the runtime role, which can operate the tables but can
-- only ever APPEND to audit_events (INSERT/SELECT — no UPDATE/DELETE), and, not
-- being the table owner, cannot ALTER tables or disable the append-only triggers.
-- Migrations keep running as the owner; deployments create a LOGIN user and GRANT
-- the runtime role TO it (compose does this via an init script).
--
-- The runtime role NAME is configurable (PORTCULLIS_RUNTIME_ROLE) because roles
-- are cluster-wide: installs sharing a cluster must not share a role. Migrate()
-- publishes it as the portcullis.runtime_role GUC; this file reads it and splices
-- it with format(%I). Standalone (psql) runs fall back to the default name.
do $$
declare
    rr text := coalesce(nullif(current_setting('portcullis.runtime_role', true), ''), 'portcullis_runtime');
begin
    -- Roles are cluster-wide while migrations run per-database: guard existence
    -- and tolerate a concurrent creation from another database's migration.
    if not exists (select 1 from pg_roles where rolname = rr) then
        begin
            execute format('create role %I nologin', rr);
        exception when duplicate_object then
            null; -- created concurrently by another database's migration
        end;
    end if;

    -- Strip PUBLIC's implicit database privileges (CONNECT, TEMPORARY, and CREATE
    -- = making new schemas), then grant back only CONNECT to the runtime role:
    -- a role from another install can't even connect, and nobody but the owner can
    -- create a TEMP or new-schema relation that could shadow the audit table by
    -- name resolution. (The owner keeps everything implicitly.)
    execute format('revoke all on database %I from public', current_database());
    execute format('grant connect on database %I to %I', current_database(), rr);

    -- Likewise strip PUBLIC's schema privileges (USAGE + CREATE) and grant the
    -- runtime only USAGE — it can use public but never CREATE an object in it.
    execute format('revoke all on schema public from public');
    execute format('grant usage on schema public to %I', rr);
    execute format('grant select, insert, update, delete on all tables in schema public to %I', rr);

    -- Audit is append-only for the runtime: read and append, never mutate.
    execute format('revoke update, delete on public.audit_events from %I', rr);

    -- Migration history is owner-only: with any access the runtime could delete
    -- applied records (forcing re-runs) or pre-insert future versions (skipping
    -- security migrations).
    execute format('revoke all on public.schema_migrations from %I', rr);

    -- Tables added by future migrations (run by the owner) get DML automatically;
    -- a migration adding a sensitive table must REVOKE in that same migration
    -- (docs/conventions/data.md).
    execute format('alter default privileges in schema public grant select, insert, update, delete on tables to %I', rr);

    -- Dev convenience: compose's init script creates the portcullis_app login user
    -- before first boot; give it the runtime role. Guarded to the DEFAULT role
    -- name only, so a custom-name (shared-cluster) install never grants the runtime
    -- role to a foreign install's login user — and the direct-login shape
    -- (PORTCULLIS_RUNTIME_ROLE=portcullis_app) is skipped, avoiding a self-grant.
    if rr = 'portcullis_runtime'
       and exists (select 1 from pg_roles where rolname = 'portcullis_app') then
        grant portcullis_runtime to portcullis_app;
    end if;
end
$$;
