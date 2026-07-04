-- name: InsertAuditEvent :exec
insert into public.audit_events (
    organization_id, actor_type, actor_user_id, actor_service,
    action, target_type, target_id, outcome, request_id, metadata
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);
