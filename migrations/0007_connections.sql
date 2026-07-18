-- 0007_connections: registered target-database connections (PRD §4.1/§6/§8.1,
-- ADR-0014). One row = a plaintext descriptor (what the list UI and archive
-- snapshots need) + one AEAD credential envelope (ADR-0003 Blob mirrored into
-- four columns; record type connection_credential). "Delete" is archive: the
-- row survives with its descriptor as the historical snapshot while the
-- credential columns are nulled in the same UPDATE.
--
-- The id has NO default on purpose: the envelope's AAD binds the record id
-- before Seal, so the application generates the UUID first (ADR-0003/0014).
create table if not exists connections (
    id                     uuid primary key,
    organization_id        uuid not null references organizations (id) on delete restrict,
    db_type                text not null check (db_type in ('postgresql', 'mysql', 'sqlite')),
    display_name           text not null,
    host                   text not null,
    port                   int  not null check (port between 1 and 65535),
    database_name          text not null,
    tls_mode               text not null check (tls_mode in ('verify-full', 'verify-ca', 'require', 'disable')),
    target_fingerprint     text not null,
    -- AEAD credential envelope (ADR-0003). key_version stays a queryable column
    -- so the eager key-rotation batch can scan rows with key_version < active.
    credential_key_version int check (credential_key_version > 0),
    credential_wrapped_dek bytea,
    credential_nonce       bytea,
    credential_ciphertext  bytea,
    created_by             uuid not null references users (id) on delete restrict,
    created_at             timestamptz not null default now(),
    updated_at             timestamptz not null default now(),
    archived_at            timestamptz, -- soft delete (data.md); no hard-delete API exists
    -- The four envelope columns travel together.
    constraint connections_credential_all_or_none check (
        (credential_key_version is null) = (credential_wrapped_dek is null)
        and (credential_key_version is null) = (credential_nonce is null)
        and (credential_key_version is null) = (credential_ciphertext is null)),
    -- Credential present exactly while active: an archived row holding a
    -- credential (PRD §4.3 requires discarding it) and an active row missing
    -- one are both unrepresentable.
    constraint connections_archived_has_no_credential check (
        archived_at is null or credential_key_version is null),
    constraint connections_active_has_credential check (
        archived_at is not null or credential_key_version is not null),
    -- Composite-unique target so future org-pinned references (policies,
    -- access requests) can use a composite FK that cannot cross organizations
    -- (ADR-0004 pattern, mirrors roles).
    unique (id, organization_id)
);

-- One ACTIVE display name per organization; archiving frees the name
-- (soft-delete-aware partial unique, data.md).
create unique index if not exists connections_org_name
    on connections (organization_id, lower(display_name)) where archived_at is null;
-- List lookups are org-scoped and usually filter archived rows.
create index if not exists connections_org_idx on connections (organization_id, archived_at);
-- FK column index (data.md).
create index if not exists connections_created_by_idx on connections (created_by);

-- Runtime-role grants (data.md review gate): NOT a sensitive/append-only table —
-- the runtime must SELECT (list/get), INSERT (create), and UPDATE (rename,
-- config replace, archive) rows, which the 0003 default privileges grant.
-- DELETE is revoked as defense-in-depth: hard delete is forbidden by the PRD
-- (§4.3/§6 — archive only, FK RESTRICT), so the runtime is made structurally
-- unable to do it. (Boot verification enforces this revoke: privcheck's
-- per-table matrix expects connections to carry S/I/U and NO DELETE.)
do $$
declare
    rr text := coalesce(nullif(current_setting('portcullis.runtime_role', true), ''), 'portcullis_runtime');
begin
    execute format('revoke delete on public.connections from %I', rr);
end
$$;
