-- Repair database and schema PUBLIC revocations for installations predating the runtime boundary changes (ADR-0009).
do $$
declare
    rr text := coalesce(nullif(current_setting('portcullis.runtime_role', true), ''), 'portcullis_runtime');
begin
    execute format('revoke all on database %I from public', current_database());
    execute format('revoke all on schema public from public');

    -- Restore the runtime floor in case a pre-rework database relied on a PUBLIC grant (explicit grants from the old 0003 make these no-ops). A missing role is the rename/rotation case: skip, and let verifyRuntimeRole guide the operator (ADR-0009).
    if exists (select 1 from pg_roles where rolname = rr) then
        execute format('grant connect on database %I to %I', current_database(), rr);
        execute format('grant usage on schema public to %I', rr);
    end if;
end
$$;
