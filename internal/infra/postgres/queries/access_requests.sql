-- Guard state transitions and draft versions. Keep the active, permitted, non-requester approval predicate aligned across counts and views (ADR-0018).

-- name: LockConnectionForRequest :one
-- FOR SHARE allows concurrent request writers while serializing archive/policy changes and their cascades against request creation (ADR-0018).
select id, organization_id, archived_at, current_policy_version, config_version, target_fingerprint
from public.connections
where id = @id and organization_id = @organization_id
for share;

-- name: ListRequestableConnections :many
-- Return active target summaries under requests.create without connection-admin permissions (ADR-0008).
select id, display_name, db_type, environment
from public.connections
where organization_id = @organization_id and archived_at is null
order by display_name asc, id asc;

-- name: InsertAccessRequest :exec
-- created_at/updated_at are given explicitly rather than left to the column DEFAULT: this insert runs after a FOR SHARE wait on the connection, and the default is now() — the transaction's start time, which predates the wait (ADR-0009).
insert into public.access_requests (
    id, organization_id, connection_id, requester_id, title,
    payload_key_version, payload_wrapped_dek, payload_nonce, payload_ciphertext,
    created_at, updated_at
) values (
    @id, @organization_id, @connection_id, @requester_id, @title,
    @payload_key_version, @payload_wrapped_dek, @payload_nonce, @payload_ciphertext,
    @at::timestamptz, @at::timestamptz
);

-- name: GetAccessRequest :one
select * from public.access_requests
where id = @id and organization_id = @organization_id;

-- name: GetAccessRequestForUpdate :one
-- The row lock every decision path takes first: concurrent approvals then serialize, so the quorum count and its transition are race-free (ADR-0018).
select * from public.access_requests
where id = @id and organization_id = @organization_id
for update;

-- name: UpdateAccessRequestDraftPayload :one
update public.access_requests
set title = @title,
    payload_key_version = @payload_key_version,
    payload_wrapped_dek = @payload_wrapped_dek,
    payload_nonce = @payload_nonce,
    payload_ciphertext = @payload_ciphertext,
    version = version + 1,
    -- The caller's observed instant, taken after it locked this row: now() is the transaction's start time and the lock may have held it for a while (ADR-0009).
    updated_at = @at::timestamptz
where id = @id and organization_id = @organization_id
  and state = 'draft' and version = @expected_version
returning *;

-- name: SubmitAccessRequest :one
-- Stamp submission, updated_at, and quorum-zero approval expiry from one post-lock database instant; reuse it in derived audit evidence.
with stamped as materialized (select clock_timestamp() as at)
update public.access_requests
set state = @state,
    statement_class = @statement_class,
    policy_version = @policy_version,
    connection_config_version = @connection_config_version,
    connection_fingerprint = @connection_fingerprint,
    connection_display_name = @connection_display_name,
    connection_db_type = @connection_db_type,
    required_approvals = @required_approvals,
    payload_digest = @payload_digest,
    payload_digest_key_version = @payload_digest_key_version,
    redacted_sql = @redacted_sql,
    -- The submission's own moment, from the same CTE as everything else here. It used to be the caller's clock, computed before this transaction existed: with any app-server skew a request could be filed as submitted AFTER the auto-approval it triggered, in the same statement that stamps updated_at from the database (ADR-0009's event-time rule).
    submitted_at = (select at from stamped),
    expires_at = case when @state::text = 'approved'
                     then (select at from stamped) + make_interval(secs => @validity_seconds::float8)
                     else null end,
    version = version + 1,
    updated_at = (select at from stamped)
where id = @id and organization_id = @organization_id
  and state = 'draft' and version = @expected_version
returning *;

-- name: TransitionAccessRequest :one
-- Use the decision’s observed instant for transition and approval expiry; otherwise clock_timestamp() avoids dating writes before lock waits.
update public.access_requests
set state = @next_state,
    state_reason = sqlc.narg('state_reason'),
    expires_at = coalesce(sqlc.narg('expires_at'), expires_at),
    version = version + 1,
    updated_at = coalesce(sqlc.narg('decided_at')::timestamptz, clock_timestamp())
where id = @id and organization_id = @organization_id and state = @from_state
returning *;

-- name: ExpireOverdueAccessRequest :many
-- Use one materialized clock_timestamp() for expiry predicate and updated_at after locking. The returned instant also dates derived audit evidence (ADR-0018).
with observed as materialized (select clock_timestamp() as at)
update public.access_requests
set state = 'expired', state_reason = 'ttl_expired', version = version + 1,
    updated_at = (select at from observed)
where id = @id and organization_id = @organization_id
  -- The window closes exactly at expires_at (access.Request.ApprovalExpiredAt, ADR-0018).
  and state = 'approved' and (expires_at is null or expires_at <= (select at from observed))
returning *;

-- name: LockSweptRequestsForConnection :many
-- Lock cascade request rows before observing time. The caller’s exclusive connection lock prevents new requests from appearing behind the sweep.
select id from public.access_requests
where connection_id = @connection_id and organization_id = @organization_id
  and state in ('draft', 'pending', 'approved')
for update;

-- name: ObserveWallClock :one
-- Observe time in a separate statement after all locks; an inline UPDATE timestamp may be evaluated before its lock wait.
select clock_timestamp()::timestamptz as at;

-- name: ExpireLiveRequestsForConnection :many
-- Expire pending/approved requests and append derived audit events in the policy/archive transaction, using the post-lock observed instant (ADR-0018).
update public.access_requests
set state = 'expired', state_reason = @state_reason, version = version + 1,
    updated_at = @at::timestamptz
where connection_id = @connection_id and organization_id = @organization_id
  and state in ('pending', 'approved')
returning *;

-- name: LockLiveRequestsForConnection :many
-- The config-replacement cascade's half of LockSweptRequestsForConnection: it touches pending/approved only, so it locks only those. Drafts survive a config change — the connection is still there and a draft carries no approval, so it can simply be submitted against the new configuration (unlike archive, which takes the connection away entirely).
select id from public.access_requests
where connection_id = @connection_id and organization_id = @organization_id
  and state in ('pending', 'approved')
for update;

-- name: CancelDraftsForConnection :many
-- Archive sweeps drafts to cancelled (§4.3: draft → cancelled on archive), on the same observed instant as the expiry above.
update public.access_requests
set state = 'cancelled', state_reason = 'connection_archived', version = version + 1,
    updated_at = @at::timestamptz
where connection_id = @connection_id and organization_id = @organization_id
  and state = 'draft'
returning *;

-- name: ExistsExecutingForConnection :one
-- The archive guard (§4.3): refuse while an execution is in flight. The state is unreachable until the execution slice; the guard is inherited (ADR-0018).
select exists (
    select 1 from public.access_requests
    where connection_id = @connection_id and organization_id = @organization_id
      and state = 'executing'
) as executing;

-- name: LockApproverMembership :many
-- Lock membership FOR SHARE before checking eligibility. Concurrent revocation serializes only once the future role-management path takes FOR UPDATE on the same row (ADR-0018).
select 1 as locked
from public.organization_memberships
where organization_id = @organization_id and user_id = @approver_id
for share;

-- name: ApproverEligible :one
-- Recheck active status and the action’s live permission inside the decision transaction; see LockApproverMembership for the revocation-lock boundary.
select exists (
    select 1
    from public.users u
    where u.id = @approver_id and u.status = 'active'
      and exists (
          select 1
          from public.organization_memberships m
          join public.roles r on r.id = m.role_id and r.deleted_at is null
          join public.role_permissions rp on rp.role_id = m.role_id and rp.deleted_at is null
          where m.organization_id = @organization_id
            and m.user_id = @approver_id
            and rp.permission_key = @permission_key
      )
) as eligible;

-- name: InsertApproval :one
-- Read clock_timestamp() after the request lock and reuse the returned instant for approval, transition, expiry, and audit.
insert into public.approvals (request_id, organization_id, approver_id, decision, reason, decided_at)
values ($1, $2, $3, $4, $5, clock_timestamp())
returning decided_at;

-- name: CountValidApprovals :one
-- How many recorded approvals still COUNT (ADR-0018): the approver is active, still resolves requests.approve through the live membership→role→permission join (the PermissionsForUser shape, rbac.sql), and is not the requester.
select count(*) from public.approvals a
join public.users u on u.id = a.approver_id and u.status = 'active'
where a.request_id = @request_id
  and a.organization_id = @organization_id
  and a.decision = 'approved'
  and a.approver_id <> @requester_id
  and exists (
      select 1
      from public.organization_memberships m
      join public.roles r on r.id = m.role_id and r.deleted_at is null
      join public.role_permissions rp on rp.role_id = m.role_id and rp.deleted_at is null
      where m.organization_id = a.organization_id
        and m.user_id = a.approver_id
        and rp.permission_key = 'requests.approve'
  );

-- name: ListApprovalsForRequest :many
-- Every decision with its display fields and computed validity (same predicate as CountValidApprovals). Rejections are listed but never "valid approvals".
select
    a.id, a.request_id, a.organization_id, a.approver_id, a.decision, a.reason, a.decided_at,
    u.email as approver_email,
    u.display_name as approver_display_name,
    (
        a.decision = 'approved'
        and u.status = 'active'
        and a.approver_id <> @requester_id
        and exists (
            select 1
            from public.organization_memberships m
            join public.roles r on r.id = m.role_id and r.deleted_at is null
            join public.role_permissions rp on rp.role_id = m.role_id and rp.deleted_at is null
            where m.organization_id = a.organization_id
              and m.user_id = a.approver_id
              and rp.permission_key = 'requests.approve'
        )
    )::boolean as valid
from public.approvals a
join public.users u on u.id = a.approver_id
where a.request_id = @request_id and a.organization_id = @organization_id
order by a.decided_at asc, a.id asc;

-- name: GetAccessRequestView :one
-- Return requester, target, and current valid-approval count together. Keep detail and list projections aligned for their shared Go row type.
select
    r.*,
    u.email as requester_email,
    u.display_name as requester_display_name,
    -- The name this request was SUBMITTED against wins; a draft has no snapshot yet, so it shows the connection as it is now (PRD §4.3 — renaming a connection must not rewrite what a past request was approved for).
    coalesce(r.connection_display_name, c.display_name) as connection_name,
    -- The badge, decided HERE. The same expression filters and counts below, so one evaluation of now() settles all three; recomputing it in the response from the app's clock let a row be counted approved and rendered expired (ADR-0018 §92), and any skew between the two machines widened the gap.
    (case when r.state = 'approved' and (r.expires_at is null or r.expires_at <= now()) then 'expired' else r.state end)::text
        as effective_state,
    -- coalesced because state_reason is null for requester/approver actions and the domain models "no reason" as the empty string, not as absence.
    coalesce(case when r.state = 'approved' and (r.expires_at is null or r.expires_at <= now()) then 'ttl_expired' else r.state_reason end, '')::text
        as effective_reason,
    (
        select count(*) from public.approvals a
        join public.users au on au.id = a.approver_id and au.status = 'active'
        where a.request_id = r.id and a.organization_id = r.organization_id
          and a.decision = 'approved' and a.approver_id <> r.requester_id
          and exists (
              select 1 from public.organization_memberships m
              join public.roles ro on ro.id = m.role_id and ro.deleted_at is null
              join public.role_permissions rp on rp.role_id = m.role_id and rp.deleted_at is null
              where m.organization_id = a.organization_id and m.user_id = a.approver_id
                and rp.permission_key = 'requests.approve'
          )
    )::bigint as valid_approvals,
    -- Count within the same query before pagination so rows, effective-state filtering, and total share one snapshot.
    count(*) over ()::bigint as total_count
from public.access_requests r
join public.users u on u.id = r.requester_id
join public.connections c on c.id = r.connection_id
where r.id = @id and r.organization_id = @organization_id;

-- name: ListAccessRequestsDesc :many
-- Filter by effective state and requester, then paginate newest-first with an ID tie-breaker; include current valid-approval counts.
select
    r.*,
    u.email as requester_email,
    u.display_name as requester_display_name,
    -- The name this request was SUBMITTED against wins; a draft has no snapshot yet, so it shows the connection as it is now (PRD §4.3 — renaming a connection must not rewrite what a past request was approved for).
    coalesce(r.connection_display_name, c.display_name) as connection_name,
    -- The badge, decided HERE. The same expression filters and counts below, so one evaluation of now() settles all three; recomputing it in the response from the app's clock let a row be counted approved and rendered expired (ADR-0018 §92), and any skew between the two machines widened the gap.
    (case when r.state = 'approved' and (r.expires_at is null or r.expires_at <= now()) then 'expired' else r.state end)::text
        as effective_state,
    -- coalesced because state_reason is null for requester/approver actions and the domain models "no reason" as the empty string, not as absence.
    coalesce(case when r.state = 'approved' and (r.expires_at is null or r.expires_at <= now()) then 'ttl_expired' else r.state_reason end, '')::text
        as effective_reason,
    (
        select count(*) from public.approvals a
        join public.users au on au.id = a.approver_id and au.status = 'active'
        where a.request_id = r.id and a.organization_id = r.organization_id
          and a.decision = 'approved' and a.approver_id <> r.requester_id
          and exists (
              select 1 from public.organization_memberships m
              join public.roles ro on ro.id = m.role_id and ro.deleted_at is null
              join public.role_permissions rp on rp.role_id = m.role_id and rp.deleted_at is null
              where m.organization_id = a.organization_id and m.user_id = a.approver_id
                and rp.permission_key = 'requests.approve'
          )
    )::bigint as valid_approvals,
    -- Count within the same query before pagination so rows, effective-state filtering, and total share one snapshot.
    count(*) over ()::bigint as total_count
from public.access_requests r
join public.users u on u.id = r.requester_id
join public.connections c on c.id = r.connection_id
where r.organization_id = @organization_id
  and (sqlc.narg('state')::text is null
       or (case when r.state = 'approved' and (r.expires_at is null or r.expires_at <= now()) then 'expired' else r.state end) = sqlc.narg('state'))
  and (sqlc.narg('requester_id')::uuid is null or r.requester_id = sqlc.narg('requester_id'))
order by r.created_at desc, r.id desc
limit @page_limit::bigint offset @row_offset::bigint;

-- name: ListAccessRequestsAsc :many
select
    r.*,
    u.email as requester_email,
    u.display_name as requester_display_name,
    -- The name this request was SUBMITTED against wins; a draft has no snapshot yet, so it shows the connection as it is now (PRD §4.3 — renaming a connection must not rewrite what a past request was approved for).
    coalesce(r.connection_display_name, c.display_name) as connection_name,
    -- The badge, decided HERE. The same expression filters and counts below, so one evaluation of now() settles all three; recomputing it in the response from the app's clock let a row be counted approved and rendered expired (ADR-0018 §92), and any skew between the two machines widened the gap.
    (case when r.state = 'approved' and (r.expires_at is null or r.expires_at <= now()) then 'expired' else r.state end)::text
        as effective_state,
    -- coalesced because state_reason is null for requester/approver actions and the domain models "no reason" as the empty string, not as absence.
    coalesce(case when r.state = 'approved' and (r.expires_at is null or r.expires_at <= now()) then 'ttl_expired' else r.state_reason end, '')::text
        as effective_reason,
    (
        select count(*) from public.approvals a
        join public.users au on au.id = a.approver_id and au.status = 'active'
        where a.request_id = r.id and a.organization_id = r.organization_id
          and a.decision = 'approved' and a.approver_id <> r.requester_id
          and exists (
              select 1 from public.organization_memberships m
              join public.roles ro on ro.id = m.role_id and ro.deleted_at is null
              join public.role_permissions rp on rp.role_id = m.role_id and rp.deleted_at is null
              where m.organization_id = a.organization_id and m.user_id = a.approver_id
                and rp.permission_key = 'requests.approve'
          )
    )::bigint as valid_approvals,
    -- Count within the same query before pagination so rows, effective-state filtering, and total share one snapshot.
    count(*) over ()::bigint as total_count
from public.access_requests r
join public.users u on u.id = r.requester_id
join public.connections c on c.id = r.connection_id
where r.organization_id = @organization_id
  and (sqlc.narg('state')::text is null
       or (case when r.state = 'approved' and (r.expires_at is null or r.expires_at <= now()) then 'expired' else r.state end) = sqlc.narg('state'))
  and (sqlc.narg('requester_id')::uuid is null or r.requester_id = sqlc.narg('requester_id'))
order by r.created_at asc, r.id asc
limit @page_limit::bigint offset @row_offset::bigint;

-- name: CountAccessRequests :one
-- Counts on the EFFECTIVE state too, so the total matches the filtered rows.
select count(*) from public.access_requests r
where r.organization_id = @organization_id
  and (sqlc.narg('state')::text is null
       or (case when r.state = 'approved' and (r.expires_at is null or r.expires_at <= now()) then 'expired' else r.state end) = sqlc.narg('state'))
  and (sqlc.narg('requester_id')::uuid is null or r.requester_id = sqlc.narg('requester_id'));
