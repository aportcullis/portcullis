-- Revoke runtime DELETE on current and future tables (ADR-0009). Any retention exception needs its own grant and explicit boot privilege policy.
do $$
declare
    rr text := coalesce(nullif(current_setting('portcullis.runtime_role', true), ''), 'portcullis_runtime');
begin
    if exists (select 1 from pg_roles where rolname = rr) then
        execute format('revoke delete on all tables in schema public from %I', rr);
        execute format('alter default privileges in schema public revoke delete on tables from %I', rr);
    end if;
end
$$;
