-- name: InsertAuditEvent :exec
insert into public.audit_events (
    organization_id, actor_type, actor_user_id, actor_service,
    action, target_type, target_id, outcome, request_id, metadata
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: GetAuditEvent :one
-- Detail remains organization-scoped; audit.get must never become an IDOR path.
select
    id, occurred_at, actor_type, actor_user_id, actor_service,
    action, target_type, target_id, outcome, request_id, metadata
from public.audit_events
where id = @id and organization_id = @organization_id;

-- name: ListAuditEventsDesc :many
-- Newest first, org-scoped; ordered by (occurred_at desc, id desc) to match the
-- audit_events_org_time_idx covering index (forward scan) and give OFFSET
-- pagination a stable tie-breaker (PRD §7.1). Extended columns
-- (state/execution/digest) are omitted until the features that populate them ship.
select
    id, occurred_at, actor_type, actor_user_id, actor_service,
    action, target_type, target_id, outcome, request_id, metadata
from public.audit_events
where organization_id = @organization_id
order by occurred_at desc, id desc
limit @page_limit::bigint offset @row_offset::bigint;

-- name: ListAuditEventsAsc :many
-- Oldest first; (occurred_at asc, id asc) is the same index scanned backward, so it
-- stays index-served and tie-breaker-stable.
select
    id, occurred_at, actor_type, actor_user_id, actor_service,
    action, target_type, target_id, outcome, request_id, metadata
from public.audit_events
where organization_id = @organization_id
order by occurred_at asc, id asc
limit @page_limit::bigint offset @row_offset::bigint;

-- name: CountAuditEvents :one
-- Total matching rows for the page controls. O(n) on a large table — PRD §7.1
-- accepts this for the audit list and defers keyset pagination to "later".
select count(*) from public.audit_events
where organization_id = @organization_id;
