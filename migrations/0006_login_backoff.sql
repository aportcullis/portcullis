-- 0006_login_backoff: per-account progressive-backoff state for password login
-- (ADR-0006 Parameters: 5 consecutive failures → 1 min lockout, doubling per
-- further failure to a 15 min cap, ±20% jitter on expiry; counter resets on
-- success or expiry).
--
-- This is CURRENT STATE, not an entity or history: one row per user, rewritten
-- in place by every failed attempt — so the soft-delete rule does not apply
-- (data.md: soft-delete is for entities; the trail of attempts lives in
-- audit_events, tagged with lockout metadata). Not sensitive: the runtime must
-- read and rewrite these rows on every failed login, so the default runtime DML
-- privileges from 0003 are exactly right — no REVOKE needed. The only lookup is
-- by user (a join from the login query and the three targeted writes), which the
-- primary key covers; no further index.
create table if not exists login_backoff (
    user_id         uuid primary key references users (id) on delete restrict,
    failure_count   int not null default 0 check (failure_count >= 0),
    locked_until    timestamptz,          -- null = not locked
    last_failure_at timestamptz not null default now()
);
