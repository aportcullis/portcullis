package crypto

const (
	// masterKeyLen is the required master key size (AES-256 / HKDF input).
	masterKeyLen = 32
	// dekLen is the size of a per-record data-encryption key.
	dekLen = 32
	// subKeyLen is the size of HKDF-derived purpose keys.
	subKeyLen = 32
	// gcmNonceLen is the standard AES-GCM nonce size (ADR-0003 pins it): Seal stores nonces of exactly this length and Open rejects anything else.
	gcmNonceLen = 12
	// csrfNonceLen is the CSPRNG nonce size inside a CSRF token (ADR-0006).
	csrfNonceLen = 16
)

// HKDF purpose labels derive distinct sub-keys from a master key version.
const (
	infoPayloadIntegrity = "portcullis/payload-integrity/v1"
	infoDEKWrap          = "portcullis/dek-wrap/v1"
)

// aadPrefix versions the canonical associated-data layout (ADR-0003): one layout for every envelope, so record identity is bound the same way everywhere.
const aadPrefix = "portcullis/aad/v1"

// AAD record types — fixed lowercase tokens, one per sealed record kind (ADR-0003 enumerates them alongside the tables they protect).
const (
	// RecordTypeOIDCPending is the OIDC pending-auth cookie (ADR-0007).
	RecordTypeOIDCPending = "oidc_pending"
	// RecordTypeAccessRequestPayload is an access request's raw SQL + typed parameter values (ADR-0018).
	RecordTypeAccessRequestPayload = "access_request_payload"
	// RecordTypeConnectionCredential is a registered connection's database login pair (ADR-0003 names this token; ADR-0014 uses it).
	RecordTypeConnectionCredential = "connection_credential"
	// RecordTypeResultSet is a result snapshot's wrapped DEK; key rotation rewraps it under the same AAD (ADR-0011).
	RecordTypeResultSet = "result_set"
	// RecordTypeResultChunk is one encrypted result chunk; its AAD also carries the chunk index and snapshot manifest (ADR-0003).
	RecordTypeResultChunk = "result_chunk"
)

// resultManifestVersion versions the manifest suffix of result chunk AAD; snapshots sealed before it was introduced fail to open and expire within the result TTL.
const resultManifestVersion = "m1"

// connectionCredentialVersion versions the JSON layout inside a sealed connection credential, so fields (e.g. a custom root CA) can be added compatibly (ADR-0014).
const connectionCredentialVersion = 1

// accessRequestPayloadVersion versions the JSON layout inside a sealed access request payload (ADR-0018).
const accessRequestPayloadVersion = 1
