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
-- User + password hash in one round-trip, so a password login costs the same
-- number of queries whether or not the account exists (anti-enumeration). The
-- hash is empty for OIDC-only users (no password row).
select u.*, coalesce(am.secret, '') as password_hash
from public.users u
left join public.auth_methods am on am.user_id = u.id and am.type = 'password'
where lower(u.email) = lower($1);

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

-- name: ExtendSessionIdle :exec
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
