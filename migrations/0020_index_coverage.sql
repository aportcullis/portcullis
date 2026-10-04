-- Index every foreign key and the hot lookups that scanned (data.md, ADR-0009); plain CREATE INDEX runs inside the migration transaction and these tables are small at this release.

-- Own-requests list: requester scope with the §7.1 newest-first tie-breaker.
create index if not exists access_requests_org_requester_created_idx
    on public.access_requests (organization_id, requester_id, created_at desc, id desc);
-- Covers the (connection_id, organization_id) and (connection_id, policy_version) foreign keys; the partial live index misses terminal rows.
create index if not exists access_requests_connection_org_idx
    on public.access_requests (connection_id, organization_id);
create index if not exists audit_events_actor_user_idx
    on public.audit_events (actor_user_id);
create index if not exists roles_organization_idx
    on public.roles (organization_id);
create index if not exists result_sets_owner_user_idx
    on result_cache.result_sets (owner_user_id);

-- Active sessions: per-user rotation and the expiry sweep read only unrevoked rows; the sweep closes expired sessions so these stay small while history rows remain (ADR-0006).
create index if not exists sessions_active_user_idx
    on public.sessions (user_id) where revoked_at is null;
create index if not exists sessions_active_idle_idx
    on public.sessions (idle_expires_at) where revoked_at is null;
