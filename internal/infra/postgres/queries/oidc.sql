-- name: FindUserBySubject :one
select u.*
from oidc_identities i
join users u on u.id = i.user_id
where i.issuer = $1 and i.subject = $2;

-- name: LinkOIDCIdentity :exec
insert into oidc_identities (user_id, issuer, subject, email)
values ($1, $2, $3, $4)
on conflict (issuer, subject) do nothing;
