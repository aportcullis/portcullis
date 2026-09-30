-- Store sealed request payloads and immutable submission snapshots. Approval evidence is append-only; current eligibility is checked when counting (ADR-0018).

create table if not exists access_requests (
    id               uuid primary key,
    organization_id  uuid not null,
    connection_id    uuid not null,
    requester_id     uuid not null references users (id) on delete restrict,
    state            text not null default 'draft' check (state in (
        'draft', 'pending', 'approved', 'rejected', 'expired', 'cancelled',
        'executing', 'succeeded', 'failed', 'outcome_unknown')),
    -- System-caused transition reasons only (requester cancels and approver rejections carry none; the rejection reason lives on the approval row).
    state_reason     text check (state_reason in (
        'policy_changed', 'connection_changed', 'connection_archived',
        'approval_invalidated', 'ttl_expired')),

    -- AEAD payload envelope (ADR-0003 shape, AAD record type access_request_payload bound to (org, request id)). Present from draft on.
    payload_key_version int   not null check (payload_key_version > 0),
    payload_wrapped_dek bytea not null,
    payload_nonce       bytea not null,
    payload_ciphertext  bytea not null,

    -- Immutable submit snapshot: NULL while draft (and on a draft that was cancelled); required in every other state (CHECK below). The digest is the keyring HMAC over the exact submitted SQL, keyed like audit_events' digest pairing.
    payload_digest             bytea,
    payload_digest_key_version int check (payload_digest_key_version is null or payload_digest_key_version > 0),
    redacted_sql       text,
    statement_class    text check (statement_class in ('read', 'write', 'ddl')),
    policy_version     bigint check (policy_version > 0),
    required_approvals int check (required_approvals between 0 and 100),
    submitted_at       timestamptz,
    -- Pin the approved config version and fingerprint so replacement invalidates approvals without erasing historical target evidence.
    connection_config_version bigint check (connection_config_version > 0),
    connection_fingerprint    text,
    -- Copy the submitted target descriptor so later connection edits cannot rewrite approval history (PRD §4.3).
    connection_display_name text,
    connection_db_type      text,
    -- Set only by the →approved transition (Nth approval / auto-approval).
    expires_at         timestamptz,

    -- Optimistic token for draft edits and submit (the connections pattern).
    version    bigint not null default 1 check (version > 0),
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),

    -- Org-pinned composite FK (the 0007 unique (id, organization_id)): a request can never cross organizations.
    foreign key (connection_id, organization_id)
        references connections (id, organization_id) on delete restrict,
    -- The policy pin is referential: version rows are append-only (0011), so the pinned snapshot (limits included) is join-stable forever and the limits are deliberately NOT copied onto this row (ADR-0018).
    foreign key (connection_id, policy_version)
        references connection_policy_versions (connection_id, version) on delete restrict,

    constraint access_requests_digest_pairing check (
        (payload_digest is null) = (payload_digest_key_version is null)),
    constraint access_requests_reason_scope check (
        state_reason is null or state in ('expired', 'cancelled')),
    -- Key snapshot completeness on submitted_at so cancelled submissions retain evidence and unsubmitted drafts carry none.
    constraint access_requests_snapshot_complete check (
        (submitted_at is null
            and payload_digest is null and payload_digest_key_version is null
            and redacted_sql is null and statement_class is null and policy_version is null
            and connection_config_version is null and connection_fingerprint is null
            and connection_display_name is null and connection_db_type is null
            and required_approvals is null
            and state in ('draft', 'cancelled'))
        or (submitted_at is not null
            and payload_digest is not null and payload_digest_key_version is not null
            and redacted_sql is not null and statement_class is not null and policy_version is not null
            and connection_config_version is not null and connection_fingerprint is not null
            and connection_display_name is not null and connection_db_type is not null
            and required_approvals is not null)),
    -- Org-pinned key so child tables (approvals) reference (id, organization_id) as a composite FK — a cross-org reference cannot be forged (data.md, ADR-0004), the same posture connections (id, organization_id) takes.
    unique (id, organization_id)
);

-- List pagination is org-scoped with the §7.1 tie-breaker; the requester index serves the own-requests scope, the partial live index serves the cascade expiries and the archive guard, and the org+state index serves list filters.
create index if not exists access_requests_org_created_idx
    on access_requests (organization_id, created_at desc, id desc);
create index if not exists access_requests_requester_idx
    on access_requests (requester_id);
create index if not exists access_requests_conn_live_idx
    on access_requests (connection_id)
    where state in ('draft', 'pending', 'approved', 'executing');
create index if not exists access_requests_org_state_idx
    on access_requests (organization_id, state);

create table if not exists approvals (
    id              uuid primary key default gen_random_uuid(),
    request_id      uuid not null,
    organization_id uuid not null,
    approver_id     uuid not null references users (id) on delete restrict,
    decision        text not null check (decision in ('approved', 'rejected')),
    -- A rejection must say why (PRD §4.4); an approval may.
    reason          text not null default '' check (char_length(reason) <= 1000),
    decided_at      timestamptz not null default now(),
    -- Composite FK pins the approval to its request AND organization together, so an approval row can never carry a foreign org id (data.md, ADR-0004).
    foreign key (request_id, organization_id)
        references access_requests (id, organization_id) on delete restrict,
    -- One decision per approver per request (§4.4); 23505 maps to ErrAlreadyDecided in the store.
    constraint approvals_one_decision_per_approver unique (request_id, approver_id),
    constraint approvals_rejection_needs_reason check (
        decision <> 'rejected' or char_length(reason) > 0)
);

create index if not exists approvals_approver_idx on approvals (approver_id);
create index if not exists approvals_org_idx on approvals (organization_id);

-- Sensitive approval evidence is append-only: revoke runtime UPDATE and DELETE; boot verification enforces SELECT/INSERT only.
do $$
declare
    rr text := coalesce(nullif(current_setting('portcullis.runtime_role', true), ''), 'portcullis_runtime');
begin
    if exists (select 1 from pg_roles where rolname = rr) then
        execute format('revoke update on public.approvals from %I', rr);
    end if;
end
$$;
