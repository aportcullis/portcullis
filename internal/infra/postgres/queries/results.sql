-- name: LockResultAdmission :exec
-- Serialize admissions per organization with the registered lock class (const.go); other organizations admit concurrently (ADR-0011).
select pg_catalog.pg_advisory_xact_lock(sqlc.arg('lock_class')::int4, sqlc.arg('lock_object')::int4);

-- name: ListResultAccounting :many
-- Read under the organization's admission lock; reads and last-access touches are not blocked, and deletes of rows a purge already removed are no-ops.
select * from result_cache.result_sets where organization_id=sqlc.arg('organization_id')
order by last_accessed_at,id;

-- name: LockExpiredResults :many
-- Purge only expired rows through result_sets_expiry_idx, skipping rows another transaction holds instead of waiting on them.
select id from result_cache.result_sets
where organization_id=sqlc.arg('organization_id') and expires_at<=clock_timestamp()
order by expires_at
limit sqlc.arg('batch_size')::int4
for update skip locked;

-- name: InsertResultChunks :copyfrom
insert into result_cache.result_chunks(result_id,organization_id,chunk_index,nonce,ciphertext)
values(sqlc.arg('result_id'),sqlc.arg('organization_id'),sqlc.arg('chunk_index'),sqlc.arg('nonce'),sqlc.arg('ciphertext'));

-- name: InsertResultSet :exec
with stamped as (select sqlc.arg('at')::timestamptz as at)
insert into result_cache.result_sets(id,organization_id,owner_user_id,row_count,byte_size,truncated,created_at,expires_at,last_accessed_at,key_version,wrapped_dek)
values(sqlc.arg('id'),sqlc.arg('organization_id'),sqlc.arg('owner_user_id'),sqlc.arg('row_count'),sqlc.arg('byte_size'),sqlc.arg('truncated'),(select at from stamped),sqlc.arg('expires_at'),(select at from stamped),sqlc.arg('key_version'),sqlc.arg('wrapped_dek'));


-- name: GetResultSet :one
select * from result_cache.result_sets
where id=sqlc.arg('id') and organization_id=sqlc.arg('organization_id')
and owner_user_id=sqlc.arg('owner_user_id') and expires_at>clock_timestamp();

-- name: GetResultChunk :one
select c.* from result_cache.result_chunks c
join result_cache.result_sets s on s.id=c.result_id and s.organization_id=c.organization_id
where s.id=sqlc.arg('result_id') and s.organization_id=sqlc.arg('organization_id')
and s.owner_user_id=sqlc.arg('owner_user_id') and s.expires_at>clock_timestamp()
and c.chunk_index=sqlc.arg('chunk_index');

-- name: TouchResultSet :exec
update result_cache.result_sets set last_accessed_at=sqlc.arg('at')::timestamptz
where id=sqlc.arg('id') and organization_id=sqlc.arg('organization_id')
and owner_user_id=sqlc.arg('owner_user_id') and expires_at>sqlc.arg('at')::timestamptz
and last_accessed_at<sqlc.arg('at')::timestamptz-interval '1 minute';

-- name: DeleteResultChunks :exec
delete from result_cache.result_chunks
where result_id=sqlc.arg('result_id') and organization_id=sqlc.arg('organization_id');

-- name: DeleteResultSet :exec
delete from result_cache.result_sets where id=sqlc.arg('id') and organization_id=sqlc.arg('organization_id');
