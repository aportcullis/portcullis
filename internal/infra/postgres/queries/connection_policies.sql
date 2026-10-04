-- name: InsertConnectionPolicyVersion :exec
-- Append-only: a policy update inserts version N+1 (the (connection_id, version) PK is the structural guard against duplicates); rows are never updated (runtime UPDATE is revoked — ADR-0015).
insert into public.connection_policy_versions (
    connection_id, organization_id, version,
    read_allowed, write_allowed, ddl_allowed,
    read_required_approvals, write_required_approvals, ddl_required_approvals,
    query_timeout_seconds, max_rows, max_result_bytes,
    created_by, created_at
) values (
    @connection_id, @organization_id, @version,
    @read_allowed, @write_allowed, @ddl_allowed,
    @read_required_approvals, @write_required_approvals, @ddl_required_approvals,
    @query_timeout_seconds, @max_rows, @max_result_bytes,
    -- Read the database clock after acquiring mutation locks so rows and audit events follow execution order; now() and inline clock_timestamp() can predate lock waits (ADR-0009). wait — named @at so the contract is visible here and not only in the store (ADR-0009). The caller's own created_at is domain-validation input.
    @created_by, @at::timestamptz
);

-- name: GetCurrentConnectionPolicy :one
-- The connection's current policy snapshot, resolved through the pointer. Works for archived connections too: the policy is part of the historical snapshot (ADR-0015).
select p.* from public.connection_policy_versions p
join public.connections c
  on c.id = p.connection_id and c.current_policy_version = p.version
where c.id = $1 and c.organization_id = $2;

-- name: BumpConnectionPolicyVersion :one
-- The optimistic pointer bump (ADR-0015): succeeds only when the caller's expected version is still current and the connection is active. Zero rows → the store disambiguates missing/archived/conflict. connections.version (the descriptor token) and updated_at are deliberately untouched — policy and descriptor concurrency are orthogonal.
update public.connections
set current_policy_version = sqlc.arg(expected_version)::bigint + 1
where id = $1 and organization_id = $2 and archived_at is null
  and current_policy_version = sqlc.arg(expected_version)::bigint
returning *;
