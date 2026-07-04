-- Grant the runtime role USAGE on sequences (ADR-0009 amendment). 0003 granted the
-- runtime role DML on tables and default DML on future tables, but NOT USAGE on
-- sequences. A table with a serial/identity column needs sequence USAGE for the
-- runtime INSERT to succeed; without it the server boots fine (the privilege check
-- does not cover sequences) and then fails at the first insert with "permission
-- denied for sequence ...". This covers existing and future sequences.
--
-- Applied migrations are immutable (data.md), so the delta ships as this new file.
-- Fresh installs run the reworked 0003 first; every statement here is idempotent,
-- so re-running is harmless. A missing role is the rename/rotation case: skip and
-- let verifyRuntimeRole guide the operator (ADR-0009).
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
