-- name: CountUsers :one
select count(*) from users;

-- name: GetDefaultOrganization :one
select * from organizations where slug = 'default';

-- name: CreateUser :one
insert into users (email, display_name)
values ($1, $2)
returning *;

-- name: GetUserByEmail :one
select * from users where lower(email) = lower($1);

-- name: GetUserByID :one
select * from users where id = $1;

-- name: CreateMembership :one
insert into organization_memberships (organization_id, user_id, role)
values ($1, $2, $3)
returning *;

-- name: GetMembership :one
select * from organization_memberships
where organization_id = $1 and user_id = $2;

-- name: UpsertPasswordAuth :exec
insert into auth_methods (user_id, type, secret)
values ($1, 'password', $2)
on conflict (user_id, type)
do update set secret = excluded.secret, updated_at = now();

-- name: GetPasswordAuth :one
select * from auth_methods where user_id = $1 and type = 'password';

-- name: CreateSession :one
insert into sessions (user_id, token_hash, idle_expires_at, absolute_expires_at)
values ($1, $2, $3, $4)
returning *;

-- name: GetSessionByTokenHash :one
select * from sessions where token_hash = $1;

-- name: RevokeSession :exec
update sessions set revoked_at = now() where id = $1;
