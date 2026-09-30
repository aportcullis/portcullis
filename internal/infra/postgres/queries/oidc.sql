-- name: FindUserBySubject :one
select u.*
from public.oidc_identities i
join public.users u on u.id = i.user_id
where i.issuer = $1 and i.subject = $2;

-- name: LinkOIDCIdentity :one
-- Idempotent only for the same user: a new (issuer, subject) inserts; an existing one owned by the same user refreshes the email; one owned by a different user matches the conflict but fails the WHERE, so no row is returned and the caller detects the collision (vs. silently succeeding).
insert into public.oidc_identities (user_id, issuer, subject, email)
values ($1, $2, $3, $4)
on conflict (issuer, subject) do update set email = excluded.email
where oidc_identities.user_id = excluded.user_id
returning user_id;
