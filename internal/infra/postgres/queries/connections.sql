-- name: InsertConnection :exec
-- current_policy_version starts at 1; the deferred FK is satisfied by the v1
-- default policy row the store inserts in the same transaction (ADR-0015).
insert into public.connections (
    id, organization_id, db_type, display_name, environment, description,
    host, port, database_name,
    tls_mode, target_fingerprint,
    credential_key_version, credential_wrapped_dek, credential_nonce, credential_ciphertext,
    created_by, created_at, updated_at, current_policy_version
) values (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9,
    $10, $11,
    $12, $13, $14, $15,
    $16, $17, $18, 1
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
-- Descriptor-only update: name, environment label, and description — no
-- credential involved. Archived rows stay editable: these fields label
-- history, not the live target.
update public.connections
set display_name = $3, environment = $4, description = $5,
    updated_at = now(), version = version + 1
where id = $1 and organization_id = $2
returning *;

-- name: ReplaceConnectionConfig :one
-- Full config replacement (ADR-0014: no partial credential edit). Archived
-- rows are excluded — restore is a separate future flow; the store
-- disambiguates "missing" from "archived" on a zero rowcount.
update public.connections
set display_name = $3, environment = $4, description = $5,
    host = $6, port = $7, database_name = $8, tls_mode = $9,
    target_fingerprint = $10,
    credential_key_version = $11, credential_wrapped_dek = $12,
    credential_nonce = $13, credential_ciphertext = $14,
    updated_at = now(), version = version + 1
where id = $1 and organization_id = $2 and archived_at is null
  and version = sqlc.arg(expected_version)
returning *;

-- name: ArchiveConnection :one
-- Archive and credential discard are ONE statement (PRD §4.3 "한 작업으로 처리").
-- The executions slice adds the in-flight-execution guard predicate here
-- (ADR-0014).
update public.connections
set archived_at = now(), updated_at = now(), version = version + 1,
    credential_key_version = null, credential_wrapped_dek = null,
    credential_nonce = null, credential_ciphertext = null
where id = $1 and organization_id = $2 and archived_at is null
returning *;

