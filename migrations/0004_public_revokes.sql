-- Re-establish the PUBLIC revocations for databases migrated before 0003 was
-- reworked (ADR-0009 amendment): those databases recorded 0003 when it only
-- revoked CONNECT from PUBLIC, so PUBLIC still holds TEMPORARY/CREATE on the
-- database and USAGE/CREATE on schema public — which verifyRuntimeRole now
-- rejects on every boot. Applied migrations are immutable (data.md), so the
-- delta ships as this new file. Fresh installs run the reworked 0003 first;
-- every statement here is idempotent, so re-running is harmless.
do $$
declare
    rr text := coalesce(nullif(current_setting('portcullis.runtime_role', true), ''), 'portcullis_runtime');
begin
    execute format('revoke all on database %I from public', current_database());
    execute format('revoke all on schema public from public');

    -- Restore the runtime floor in case a pre-rework database relied on a PUBLIC
    -- grant (explicit grants from the old 0003 make these no-ops). A missing role
    -- is the rename/rotation case: skip, and let verifyRuntimeRole guide the
    -- operator (ADR-0009).
    if exists (select 1 from pg_roles where rolname = rr) then
        execute format('grant connect on database %I to %I', current_database(), rr);
        execute format('grant usage on schema public to %I', rr);
    end if;
end
$$;
