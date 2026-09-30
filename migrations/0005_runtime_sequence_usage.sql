-- Grant sequence USAGE for existing and future runtime inserts. Skip a missing role so boot verification can report rename or rotation drift (ADR-0009).
do $$
declare
    rr text := coalesce(nullif(current_setting('portcullis.runtime_role', true), ''), 'portcullis_runtime');
begin
    if exists (select 1 from pg_roles where rolname = rr) then
        execute format('grant usage on all sequences in schema public to %I', rr);
        execute format('alter default privileges in schema public grant usage on sequences to %I', rr);
    end if;
end
$$;
