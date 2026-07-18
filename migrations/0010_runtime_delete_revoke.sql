-- 0010_runtime_delete_revoke: the runtime role holds hard DELETE on NO table
-- (ADR-0009 amendment; external review).
--
-- Every entity is soft-delete-only and hard delete is a separate retention
-- concern (docs/conventions/data.md); not a single runtime query issues
-- DELETE. The 0003-era blanket grant therefore violated least privilege
-- (OWASP Database Security): an application SQL defect or a compromised
-- process could permanently destroy users, roles, or sessions. This revokes
-- DELETE from every current table and from the owner's default privileges, so
-- tables created by future migrations arrive without it. Boot verification's
-- default per-table policy forbids DELETE from the same commit, so a regressed
-- grant fails the next start.
--
-- A table that genuinely needs runtime DELETE (e.g. a future result-cache TTL
-- eviction, ADR-0011) must GRANT it in its own migration and register an
-- explicit tablePolicies exception — the same review gate sensitive tables
-- already go through.
--
-- Runs as the schema owner (Migrate SET ROLEs, ADR-0009), the role the 0003/
-- 0008 per-schema default-privilege grants are bound to — a per-schema ADP
-- REVOKE exactly reverses a previous per-schema ADP GRANT for that role
-- (web-verified: PostgreSQL ALTER DEFAULT PRIVILEGES).
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
