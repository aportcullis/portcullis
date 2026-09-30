-- Store mutable current login-failure state per user; attempt history belongs to audit_events. Default runtime read/write grants apply (ADR-0006).
create table if not exists login_backoff (
    user_id         uuid primary key references users (id) on delete restrict,
    failure_count   int not null default 0 check (failure_count >= 0),
    locked_until    timestamptz,          -- null = not locked
    last_failure_at timestamptz not null default now()
);
