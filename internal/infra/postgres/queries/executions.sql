-- name: InsertQueryExecution :one
with stamp as (select sqlc.arg('at')::timestamptz as at)
insert into public.query_executions (request_id, organization_id, owner, attempt_id, heartbeat, deadline, started_at)
select sqlc.arg('request_id'), sqlc.arg('organization_id'), sqlc.arg('owner'), sqlc.arg('attempt_id'), stamp.at, stamp.at + sqlc.arg('lease_milliseconds')::bigint * interval '1 millisecond', stamp.at from stamp
returning *;

-- name: LockQueryExecution :one
select * from public.query_executions
where request_id = sqlc.arg('request_id') and organization_id = sqlc.arg('organization_id')
for update;

-- name: GetQueryExecution :one
select * from public.query_executions
where request_id = sqlc.arg('request_id') and organization_id = sqlc.arg('organization_id');

-- name: HeartbeatQueryExecution :execrows
update public.query_executions set heartbeat = sqlc.arg('at')::timestamptz, deadline = sqlc.arg('at')::timestamptz + sqlc.arg('lease_milliseconds')::bigint * interval '1 millisecond'
where request_id = sqlc.arg('request_id') and organization_id = sqlc.arg('organization_id')
  and owner = sqlc.arg('owner') and attempt_id = sqlc.arg('attempt_id') and outcome is null
  and deadline > sqlc.arg('at')::timestamptz;

-- name: FinishQueryExecution :execrows
update public.query_executions
set outcome = sqlc.arg('outcome'), finished_at = sqlc.arg('at')::timestamptz,
    rows_affected = sqlc.arg('rows_affected'), duration_ms = sqlc.arg('duration_ms'),
    result_id = sqlc.narg('result_id'), result_expires_at = sqlc.narg('result_expires_at'),
    row_count = sqlc.arg('row_count'), byte_count = sqlc.arg('byte_count'), truncated = sqlc.arg('truncated')
where request_id = sqlc.arg('request_id') and organization_id = sqlc.arg('organization_id')
  and owner = sqlc.arg('owner') and attempt_id = sqlc.arg('attempt_id') and outcome is null;

-- name: ListOverdueExecutions :many
select request_id from public.query_executions
where organization_id = sqlc.arg('organization_id') and outcome is null and deadline <= clock_timestamp()
order by request_id limit sqlc.arg('batch_size')::int;
