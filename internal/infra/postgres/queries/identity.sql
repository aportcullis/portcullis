-- name: CountUsers :one
select count(*) from public.users;

-- name: GetDefaultOrganization :one
select * from public.organizations where slug = 'default';

-- name: CreateUser :one
insert into public.users (email, display_name)
values ($1, $2)
returning *;

-- name: GetUserByEmail :one
select * from public.users where lower(email) = lower($1);

-- name: GetUserForLogin :one
-- User + password hash + progressive-backoff state in one round-trip, so a
-- password login costs the same number of queries whether or not the account
-- exists (anti-enumeration). The hash is empty for OIDC-only users (no password
-- row); the backoff columns are zero/null for accounts that never failed. The
-- locked flag is evaluated HERE on the database clock — the same clock the
-- failure upsert uses — so app/DB clock skew can't split the expiry decision.
select u.*, coalesce(am.secret, '') as password_hash,
       coalesce(lb.failure_count, 0) as failure_count,
       lb.locked_until,
       (lb.locked_until is not null and lb.locked_until > now())::boolean as locked
from public.users u
left join public.auth_methods am on am.user_id = u.id and am.type = 'password'
left join public.login_backoff lb on lb.user_id = u.id
where lower(u.email) = lower($1);

-- name: RecordLoginFailure :one
-- ONE atomic statement per failed attempt (ADR-0006): bump the counter and,
-- at/after the threshold, impose or extend the jittered lockout — so a
-- concurrent success reset or lazy expiry-reset can never interleave between
-- counting and locking, and every failure costs exactly one write. All time
-- arithmetic runs on the database clock. The counter restarts at 1 when the
-- previous lockout has expired ("resets on expiry", applied lazily) or when
-- the last failure is older than the staleness window (months-old typos must
-- not count toward a fresh lockout). The window is
-- min(base·2^(n-threshold), cap)·jitter_factor with the exponent clamped to
-- [0,30], so no operand can overflow; greatest() keeps a concurrent shorter
-- jittered window from moving an existing lockout backward.
insert into public.login_backoff (user_id, failure_count, locked_until, last_failure_at)
values (
    sqlc.arg(user_id), 1,
    case when 1 >= sqlc.arg(threshold)::int then
        now() + make_interval(secs =>
            least(sqlc.arg(base_secs)::float8, sqlc.arg(cap_secs)::float8)
            * sqlc.arg(jitter_factor)::float8)
    end,
    now()
)
on conflict (user_id) do update set
    failure_count = case
        when (login_backoff.locked_until is not null and login_backoff.locked_until <= now())
          or (login_backoff.locked_until is null and login_backoff.last_failure_at <= now() - make_interval(secs => sqlc.arg(staleness_secs)::float8))
        then 1 else login_backoff.failure_count + 1 end,
    locked_until = case
        when (case
                when (login_backoff.locked_until is not null and login_backoff.locked_until <= now())
                  or (login_backoff.locked_until is null and login_backoff.last_failure_at <= now() - make_interval(secs => sqlc.arg(staleness_secs)::float8))
                then 1 else login_backoff.failure_count + 1 end) >= sqlc.arg(threshold)::int
        then greatest(
            coalesce(case when login_backoff.locked_until > now() then login_backoff.locked_until end, '-infinity'::timestamptz),
            now() + make_interval(secs =>
                least(
                    sqlc.arg(base_secs)::float8 * pow(2, least(greatest(
                        (case
                            when (login_backoff.locked_until is not null and login_backoff.locked_until <= now())
                              or (login_backoff.locked_until is null and login_backoff.last_failure_at <= now() - make_interval(secs => sqlc.arg(staleness_secs)::float8))
                            then 1 else login_backoff.failure_count + 1 end) - sqlc.arg(threshold)::int, 0), 30)),
                    sqlc.arg(cap_secs)::float8)
                * sqlc.arg(jitter_factor)::float8))
        else null end,
    last_failure_at = now()
returning failure_count, locked_until,
    (locked_until is not null and locked_until > now())::boolean as locked;

-- name: ResetLoginBackoff :exec
-- A successful login clears the slate. The WHERE leaves an already-clean row
-- unwritten, so calling this on every success keeps the hot path write-free
-- while still clearing failures committed by concurrent attempts mid-verify.
update public.login_backoff
set failure_count = 0, locked_until = null
where user_id = $1 and (failure_count > 0 or locked_until is not null);

-- name: GetUserByID :one
select * from public.users where id = $1;

-- name: CreateMembership :one
insert into public.organization_memberships (organization_id, user_id, role_id)
values ($1, $2, $3)
returning *;

-- name: GetMembership :one
select * from public.organization_memberships
where organization_id = $1 and user_id = $2;

-- name: UpsertPasswordAuth :exec
insert into public.auth_methods (user_id, type, secret)
values ($1, 'password', $2)
on conflict (user_id, type)
do update set secret = excluded.secret, updated_at = now();

-- name: GetPasswordAuth :one
select * from public.auth_methods where user_id = $1 and type = 'password';

-- name: CreateSession :one
insert into public.sessions (user_id, token_hash, idle_expires_at, absolute_expires_at)
values ($1, $2, $3, $4)
returning *;

-- name: GetSessionByTokenHash :one
select * from public.sessions where token_hash = $1;

-- name: ValidateSession :one
-- Final post-CSRF validity check for a request whose idle slide is throttled.
-- It deliberately does not write, but its predicates use the database clock so
-- a concurrently revoked or expired session cannot reach a handler.
select true from public.sessions
where id = $1
  and revoked_at is null
  and idle_expires_at > now()
  and absolute_expires_at > now();

-- name: RevokeSession :execrows
-- Only an ACTIVE session revokes: re-revoking (a concurrent double logout) must
-- not overwrite the original revoked_at — forensic evidence of WHEN the session
-- actually died — and the caller skips the audit event when no row changed, so
-- the trail records only real state changes (ADR-0009).
update public.sessions set revoked_at = now()
where id = $1 and revoked_at is null;

-- name: RevokeUserSessions :exec
-- Invalidate a user's active sessions (ADR-0006: login/privilege change rotates).
update public.sessions set revoked_at = now()
where user_id = $1 and revoked_at is null;

-- name: ExtendSessionIdle :execrows
-- Slide the idle window forward on activity, never past the absolute expiry and
-- never backward (greatest() guards against a late, older request regressing it).
-- Re-check both expiries in the write: a session can expire after Authenticate
-- reads it but before the post-CSRF slide, and an expired session must never be
-- resurrected by that race.
update public.sessions
set idle_expires_at = least(greatest(idle_expires_at, sqlc.arg(idle_expires_at)), absolute_expires_at)
where id = sqlc.arg(id)
  and revoked_at is null
  and idle_expires_at > now()
  and absolute_expires_at > now();
