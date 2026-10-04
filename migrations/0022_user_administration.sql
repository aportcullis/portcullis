-- 0022_user_administration: one-time password setup links, optimistic role versions and soft-deleted role permissions (ADR-0053).

-- Optimistic concurrency token for role updates and deletes.
alter table public.roles
    add column if not exists version bigint not null default 1 check (version > 0);

-- One-time password setup links. Only the SHA-256 digest of the 32-byte token is stored. Consume and revoke are UPDATEs of current state; the audit log holds the evidence, so the default runtime policy (no DELETE) applies.
create table if not exists public.password_setup_tokens (
    id               uuid primary key default gen_random_uuid(),
    organization_id  uuid not null references public.organizations (id) on delete restrict,
    user_id          uuid not null references public.users (id) on delete restrict,
    token_hash       bytea not null unique check (octet_length(token_hash) = 32),
    expires_at       timestamptz not null,
    consumed_at      timestamptz,
    revoked_at       timestamptz,
    created_at       timestamptz not null,
    constraint password_setup_tokens_single_outcome check (consumed_at is null or revoked_at is null),
    constraint password_setup_tokens_expiry_after_creation check (expires_at > created_at)
);
-- At most one open link per user; consumed and revoked rows keep their history.
create unique index if not exists password_setup_tokens_one_open
    on public.password_setup_tokens (user_id) where consumed_at is null and revoked_at is null;
create index if not exists password_setup_tokens_user_idx on public.password_setup_tokens (user_id);
create index if not exists password_setup_tokens_organization_idx on public.password_setup_tokens (organization_id);

-- A permission removed from a role is soft-deleted and a re-added one is revived on the same (role_id, permission_key) row, so the runtime keeps no DELETE grant (docs/conventions/data.md); every permission read ignores deleted rows.
alter table public.role_permissions
    add column if not exists deleted_at timestamptz;
