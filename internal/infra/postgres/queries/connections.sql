-- name: InsertConnection :exec
-- Create policy v1 in the same transaction to satisfy the deferred FK. Use post-lock database time for row and audit ordering (ADR-0009).
insert into public.connections (
    id, organization_id, db_type, display_name, environment, description,
    host, port, database_name,
    tls_mode, target_fingerprint,
    credential_key_version, credential_wrapped_dek, credential_nonce, credential_ciphertext,
    created_by, created_at, updated_at, current_policy_version
) values (
    @id, @organization_id, @db_type, @display_name, @environment, @description,
    @host, @port, @database_name,
    @tls_mode, @target_fingerprint,
    @credential_key_version, @credential_wrapped_dek, @credential_nonce, @credential_ciphertext,
    @created_by, @at::timestamptz, @at::timestamptz, 1
);

-- name: GetConnection :one
select * from public.connections
where id = $1 and organization_id = $2;

-- name: ListConnections :many
select * from public.connections
where organization_id = $1
  and (sqlc.arg(include_archived)::boolean or archived_at is null)
order by created_at desc, id desc;

-- name: UpdateConnectionDescriptor :one
-- Replace descriptor fields at the editor’s version, including archived history labels. Stamp with the post-lock observed instant; zero rows mean missing or stale.
update public.connections
set display_name = @display_name, environment = @environment, description = @description,
    updated_at = @at::timestamptz, version = version + 1
where id = @id and organization_id = @organization_id
  and version = sqlc.arg(expected_version)
returning *;

-- name: ReplaceConnectionConfig :one
-- Full config replacement (ADR-0014: no partial credential edit). Archived rows are excluded — restore is a separate future flow; the store disambiguates "missing" from "archived" on a zero rowcount.
update public.connections
set display_name = @display_name, environment = @environment, description = @description,
    host = @host, port = @port, database_name = @database_name, tls_mode = @tls_mode,
    target_fingerprint = @target_fingerprint,
    credential_key_version = @credential_key_version, credential_wrapped_dek = @credential_wrapped_dek,
    credential_nonce = @credential_nonce, credential_ciphertext = @credential_ciphertext,
    updated_at = @at::timestamptz, version = version + 1,
    -- The TARGET changed, so the target token moves too — this is the only statement that touches it. Requests approved against the old configuration pin the old value and are expired in this same transaction (ADR-0018); a rename must not do that, which is why this is not `version`.
    config_version = config_version + 1
where id = @id and organization_id = @organization_id and archived_at is null
  and version = sqlc.arg(expected_version)
returning *;

-- name: LockConnectionForWrite :one
-- Lock before observing time so mutation timestamps follow lock waits. Updates retain their own archive predicates for precise refusal reasons.
select id from public.connections
where id = @id and organization_id = @organization_id
for update;

-- name: ArchiveConnection :one
-- Archive and credential discard are ONE statement (PRD §4.3 "한 작업으로 처리"). The executions slice adds the in-flight-execution guard predicate here (ADR-0014). The stamp is the caller's observed instant — the same one its request cascade uses — never now(), which is this transaction's start time and may predate the lock wait entirely (ADR-0009).
update public.connections
set archived_at = @at::timestamptz, updated_at = @at::timestamptz, version = version + 1,
    credential_key_version = null, credential_wrapped_dek = null,
    credential_nonce = null, credential_ciphertext = null
where id = @id and organization_id = @organization_id and archived_at is null
returning *;
