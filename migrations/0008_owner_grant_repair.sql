-- 0008_owner_grant_repair: re-anchor runtime grants and default privileges to
-- the schema owner (ADR-0009 amendment; external review).
--
-- ALTER DEFAULT PRIVILEGES binds to the role that runs it and is NEVER
-- inherited through membership. 0003/0005 bound theirs to whatever principal
-- migrated back then; on a database migrated by a member-of-owner (or a
-- superuser that is not datdba), tables created by LATER migrations would get
-- no runtime grants. Migrate() now SET ROLEs to the schema owner before
-- applying migrations, and this file — running as that owner — repairs any
-- database whose grants/ADP were bound to a previous migrator:
--   1. re-grant DML on everything that exists today,
--   2. re-bind the default privileges to the owner (covers future migrations),
--   3. re-apply every sensitive-table revoke the blanket grant re-granted
--      (the transient re-grant is confined to this migration's transaction).
-- Boot verification now checks the full per-table matrix, so a repaired or
-- healthy database proves itself on every start.
--
-- Residual limitation (documented in ADR-0009): TABLE OWNERSHIP of relations
-- created by a previous member-migrator is not normalized here (the owner
-- cannot ALTER tables it does not own) — a future migration ALTERing such a
-- table needs a manual REASSIGN OWNED. Grants, the actual failure class, are
-- fully repaired.
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
