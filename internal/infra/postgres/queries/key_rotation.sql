-- name: LockRotationCredentials :many
select id,organization_id,credential_key_version,credential_wrapped_dek,credential_nonce,credential_ciphertext
from public.connections where organization_id=sqlc.arg('organization_id') and credential_key_version < sqlc.arg('active_version')::int
order by id limit 100 for update;

-- name: RotateCredentialEnvelope :exec
update public.connections set credential_key_version=sqlc.arg('key_version'),credential_wrapped_dek=sqlc.arg('wrapped_dek'),
credential_nonce=sqlc.arg('nonce'),credential_ciphertext=sqlc.arg('ciphertext')
where id=sqlc.arg('id') and organization_id=sqlc.arg('organization_id');

-- name: LockRotationPayloads :many
select id,organization_id,payload_key_version,payload_wrapped_dek,payload_nonce,payload_ciphertext
from public.access_requests where organization_id=sqlc.arg('organization_id') and payload_key_version < sqlc.arg('active_version')::int
order by id limit 100 for update;

-- name: RotatePayloadEnvelope :exec
update public.access_requests set payload_key_version=sqlc.arg('key_version'),payload_wrapped_dek=sqlc.arg('wrapped_dek'),
payload_nonce=sqlc.arg('nonce'),payload_ciphertext=sqlc.arg('ciphertext')
where id=sqlc.arg('id') and organization_id=sqlc.arg('organization_id');

-- name: LockRotationResultKeys :many
select id,organization_id,key_version,wrapped_dek from result_cache.result_sets
where organization_id=sqlc.arg('organization_id') and key_version < sqlc.arg('active_version')::int order by id limit 100 for update;

-- name: RotateResultEnvelope :exec
update result_cache.result_sets set key_version=sqlc.arg('key_version'),wrapped_dek=sqlc.arg('wrapped_dek')
where id=sqlc.arg('id') and organization_id=sqlc.arg('organization_id');
