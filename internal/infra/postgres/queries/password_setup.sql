-- name: ResolvePasswordSetup :one
select token.user_id,token.organization_id,token.expires_at
from public.password_setup_tokens token
join public.organization_memberships member on member.organization_id=token.organization_id and member.user_id=token.user_id
join public.users account on account.id=member.user_id and account.status='active'
where token.token_hash=sqlc.arg(token_hash)::bytea and token.organization_id=sqlc.arg(organization_id)::uuid
 and token.consumed_at is null and token.revoked_at is null and token.expires_at>clock_timestamp()
 and not exists(select 1 from public.auth_methods method where method.user_id=token.user_id and method.type='password');

-- name: ConsumePasswordSetup :execrows
update public.password_setup_tokens set consumed_at=sqlc.arg(at)::timestamptz
where token_hash=sqlc.arg(token_hash)::bytea and organization_id=sqlc.arg(organization_id)::uuid and user_id=sqlc.arg(user_id)::uuid
 and consumed_at is null and revoked_at is null and expires_at>sqlc.arg(at)::timestamptz;

-- name: InsertInitialPassword :execrows
insert into public.auth_methods(user_id,type,secret,created_at,updated_at)
values(sqlc.arg(user_id)::uuid,'password',sqlc.arg(secret)::text,sqlc.arg(at)::timestamptz,sqlc.arg(at)::timestamptz)
on conflict(user_id,type) do nothing;

-- name: RevokePasswordSetupSessions :execrows
update public.sessions set revoked_at=sqlc.arg(at)::timestamptz
where user_id=sqlc.arg(user_id)::uuid and revoked_at is null;
