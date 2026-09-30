-- Rebind existing and future runtime grants to the schema owner, then restore sensitive-table revokes (ADR-0009). Historical table ownership still requires manual REASSIGN OWNED.
do $$
declare
    rr text := coalesce(nullif(current_setting('portcullis.runtime_role', true), ''), 'portcullis_runtime');
begin
    if exists (select 1 from pg_roles where rolname = rr) then
        execute format('grant select, insert, update, delete on all tables in schema public to %I', rr);
        execute format('grant usage on all sequences in schema public to %I', rr);
        execute format('alter default privileges in schema public grant select, insert, update, delete on tables to %I', rr);
        execute format('alter default privileges in schema public grant usage on sequences to %I', rr);
        -- Sensitive-table boundary (docs/conventions/data.md): re-apply.
        execute format('revoke update, delete on public.audit_events from %I', rr);
        execute format('revoke all on public.schema_migrations from %I', rr);
        execute format('revoke delete on public.connections from %I', rr);
    end if;
end
$$;
