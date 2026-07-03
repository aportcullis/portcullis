-- 0001_init: foundation tables for identity, sessions, and the unified audit log.

create table if not exists organizations (
    id          uuid primary key default gen_random_uuid(),
    name        text not null,
    slug        text not null unique,
    created_at  timestamptz not null default now()
);

create table if not exists users (
    id            uuid primary key default gen_random_uuid(),
    email         text not null,
    display_name  text not null default '',
    status        text not null default 'active' check (status in ('active', 'disabled')),
    created_at    timestamptz not null default now()
);
create unique index if not exists users_email_lower_idx on users (lower(email));

create table if not exists organization_memberships (
    id               uuid primary key default gen_random_uuid(),
    organization_id  uuid not null references organizations (id) on delete restrict,
    user_id          uuid not null references users (id) on delete restrict,
    role             text not null check (role in ('admin', 'approver', 'requester')),
    created_at       timestamptz not null default now(),
    unique (organization_id, user_id)
);

create table if not exists auth_methods (
    id          uuid primary key default gen_random_uuid(),
    user_id     uuid not null references users (id) on delete restrict,
    type        text not null check (type in ('password')),
    secret      text not null, -- PHC-encoded argon2id hash for password auth
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now(),
    unique (user_id, type)
);

create table if not exists sessions (
    id                   uuid primary key default gen_random_uuid(),
    user_id              uuid not null references users (id) on delete restrict,
    token_hash           bytea not null unique,
    idle_expires_at      timestamptz not null,
    absolute_expires_at  timestamptz not null,
    revoked_at           timestamptz,
    created_at           timestamptz not null default now()
);
create index if not exists sessions_user_idx on sessions (user_id);

create table if not exists audit_events (
    id               uuid primary key default gen_random_uuid(),
    organization_id  uuid not null references organizations (id) on delete restrict,
    occurred_at      timestamptz not null default now(),
    actor_type       text not null check (actor_type in ('user', 'system', 'service')),
    actor_user_id    uuid references users (id) on delete restrict,
    actor_service    text,
    action           text not null,
    target_type      text not null,
    target_id        text,
    outcome          text not null,
    previous_state   text,
    next_state       text,
    payload_digest   bytea,
    -- Key version of the HMAC key that produced payload_digest, so the digest
    -- stays verifiable across master-key rotations (ADR-0003). The pair is
    -- all-or-nothing: a digest without its key version is unverifiable, and a
    -- version without a digest is meaningless.
    payload_digest_key_version int
        check (payload_digest_key_version is null or payload_digest_key_version > 0),
    request_id       text,
    connection_id    uuid,
    query_type       text,
    rows_affected    bigint,
    duration_ms      bigint,
    risk_score       double precision,
    metadata         jsonb not null default '{}'::jsonb,
    constraint audit_digest_pairing
        check ((payload_digest is null) = (payload_digest_key_version is null))
);
create index if not exists audit_events_org_time_idx on audit_events (organization_id, occurred_at desc, id desc);

-- audit_events is append-only: block UPDATE, DELETE, and TRUNCATE at the database
-- level so history cannot be rewritten even by the application runtime.
create or replace function audit_events_block_mutation() returns trigger
language plpgsql as $$
begin
    raise exception 'audit_events is append-only';
end;
$$;

create or replace trigger audit_events_no_mutation
before update or delete on audit_events
for each row execute function audit_events_block_mutation();

-- Row-level triggers do not fire on TRUNCATE, so block it with a statement-level trigger.
create or replace trigger audit_events_no_truncate
before truncate on audit_events
for each statement execute function audit_events_block_mutation();

-- Seed the single self-hosted organization.
insert into organizations (slug, name) values ('default', 'Default')
on conflict (slug) do nothing;
