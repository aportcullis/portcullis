-- Store first-run setup tokens as SHA-256 hashes only (ADR-0052). Classification: installation-level current state, not audit evidence; the runtime role reads, inserts and soft-revokes or consumes rows through UPDATE, and the global DELETE revoke (0010) keeps rows from being erased.
create table if not exists public.setup_tokens (
    id          uuid primary key default gen_random_uuid(),
    token_hash  bytea not null check (octet_length(token_hash) = 32),
    created_at  timestamptz not null default now(),
    expires_at  timestamptz not null,
    consumed_at timestamptz,
    deleted_at  timestamptz,
    constraint setup_tokens_expiry_after_creation check (expires_at > created_at),
    constraint setup_tokens_single_terminal_state check (consumed_at is null or deleted_at is null)
);

create unique index if not exists setup_tokens_token_hash_key on public.setup_tokens (token_hash);

-- At most one token can be outstanding: issuing a new one soft-revokes its predecessor in the same transaction.
create unique index if not exists setup_tokens_one_outstanding on public.setup_tokens ((true))
    where consumed_at is null and deleted_at is null;
