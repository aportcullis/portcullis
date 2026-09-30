-- The runtime role may append/read audit but cannot mutate evidence or schema. Migrations use the owner and read the install-specific role name from a GUC (ADR-0009).
do $$
declare
    rr text := coalesce(nullif(current_setting('portcullis.runtime_role', true), ''), 'portcullis_runtime');
begin
    -- Roles are cluster-wide while migrations run per-database: guard existence and tolerate a concurrent creation from another database's migration.
    if not exists (select 1 from pg_roles where rolname = rr) then
        begin
            execute format('create role %I nologin', rr);
        exception when duplicate_object then
            null; -- created concurrently by another database's migration
        end;
    end if;

    -- Revoke PUBLIC database privileges and grant runtime CONNECT only, preventing temporary or new-schema relations from shadowing audit tables.
    execute format('revoke all on database %I from public', current_database());
    execute format('grant connect on database %I to %I', current_database(), rr);

    -- Likewise strip PUBLIC's schema privileges (USAGE + CREATE) and grant the runtime only USAGE — it can use public but never CREATE an object in it.
    execute format('revoke all on schema public from public');
    execute format('grant usage on schema public to %I', rr);
    execute format('grant select, insert, update, delete on all tables in schema public to %I', rr);

    -- Audit is append-only for the runtime: read and append, never mutate.
    execute format('revoke update, delete on public.audit_events from %I', rr);

    -- Migration history is owner-only: with any access the runtime could delete applied records (forcing re-runs) or pre-insert future versions (skipping security migrations).
    execute format('revoke all on public.schema_migrations from %I', rr);

    -- Tables added by future migrations (run by the owner) get DML automatically; a migration adding a sensitive table must REVOKE in that same migration (docs/conventions/data.md).
    execute format('alter default privileges in schema public grant select, insert, update, delete on tables to %I', rr);

    -- Grant the default runtime role to compose’s app user when permitted; custom installations must configure membership themselves.
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
