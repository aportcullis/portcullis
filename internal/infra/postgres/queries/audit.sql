-- name: InsertAuditEvent :exec
-- Ordinary events use database time; derived events reuse the observation instant so expiry audit cannot precede its deadline (ADR-0009).
insert into public.audit_events (
    organization_id, occurred_at, actor_type, actor_user_id, actor_service,
    action, target_type, target_id, outcome, request_id,
    previous_state, next_state, connection_id, query_type,
    payload_digest, payload_digest_key_version, metadata, rows_affected, duration_ms
) values ($1, coalesce(sqlc.narg('occurred_at')::timestamptz, now()), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18);

-- name: InsertAuditEvents :batchexec
-- The same insert sent as one pipelined batch, so a cascade appends its derived events in one round trip while holding row locks (ADR-0009).
insert into public.audit_events (
    organization_id, occurred_at, actor_type, actor_user_id, actor_service,
    action, target_type, target_id, outcome, request_id,
    previous_state, next_state, connection_id, query_type,
    payload_digest, payload_digest_key_version, metadata, rows_affected, duration_ms
) values ($1, coalesce(sqlc.narg('occurred_at')::timestamptz, now()), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18);

-- name: GetAuditEvent :one
-- Detail remains organization-scoped; audit.get must never become an IDOR path. total_count is literally 1 here and goes unused — it keeps the row shape structurally identical to the list rows, so one mapper serves all three.
select
    id, occurred_at, actor_type, actor_user_id, actor_service,
    action, target_type, target_id, outcome, request_id,
    previous_state, next_state, connection_id, query_type,
    payload_digest, payload_digest_key_version, metadata, rows_affected, duration_ms,
    1::bigint as total_count
from public.audit_events
where id = @id and organization_id = @organization_id;

-- name: ListAuditEventsDesc :many
-- Newest first, org-scoped; ordered by (occurred_at desc, id desc) to match the audit_events_org_time_idx covering index (forward scan) and give OFFSET pagination a stable tie-breaker (PRD §7.1). The state/execution/digest columns are populated from the access-request slice on (ADR-0018).
select
    id, occurred_at, actor_type, actor_user_id, actor_service,
    action, target_type, target_id, outcome, request_id,
    previous_state, next_state, connection_id, query_type,
    payload_digest, payload_digest_key_version, metadata, rows_affected, duration_ms,
    -- Count within the same query before pagination so rows, effective-state filtering, and total share one snapshot.
    count(*) over ()::bigint as total_count
from public.audit_events
where organization_id = @organization_id
order by occurred_at desc, id desc
limit @page_limit::bigint offset @row_offset::bigint;

-- name: ListAuditEventsAsc :many
-- Oldest first; (occurred_at asc, id asc) is the same index scanned backward, so it stays index-served and tie-breaker-stable.
select
    id, occurred_at, actor_type, actor_user_id, actor_service,
    action, target_type, target_id, outcome, request_id,
    previous_state, next_state, connection_id, query_type,
    payload_digest, payload_digest_key_version, metadata, rows_affected, duration_ms,
    count(*) over ()::bigint as total_count
from public.audit_events
where organization_id = @organization_id
order by occurred_at asc, id asc
limit @page_limit::bigint offset @row_offset::bigint;

-- name: CountAuditEvents :one
-- The empty-page fallback: the total normally rides the list rows (the window count above), but an empty page has no row to carry it. Only ever run inside the same read snapshot as the list, never as a standalone statement. O(n) on a large table — PRD §7.1 accepts this and defers keyset pagination to "later".
select count(*) from public.audit_events
where organization_id = @organization_id;
