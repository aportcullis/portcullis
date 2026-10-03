-- Cached results are intentionally disposable on PostgreSQL crash or failover.
create schema if not exists result_cache;
revoke all on schema result_cache from public;
create unlogged table if not exists result_cache.result_sets (
 id uuid primary key,
 organization_id uuid not null references public.organizations(id) on delete restrict,
 owner_user_id uuid not null references public.users(id) on delete restrict,
 row_count bigint not null check (row_count between 0 and 10000),
 byte_size bigint not null check (byte_size between 0 and 26214400),
	truncated boolean not null default false,
 created_at timestamptz not null,
 expires_at timestamptz not null,
 last_accessed_at timestamptz not null,
 key_version int not null check (key_version > 0),
 wrapped_dek bytea not null,
 unique(id,organization_id)
) with (autovacuum_vacuum_scale_factor=0.02,autovacuum_analyze_scale_factor=0.05,autovacuum_vacuum_cost_delay=0);
create index if not exists result_sets_owner_idx on result_cache.result_sets(organization_id,owner_user_id,last_accessed_at,id);
create index if not exists result_sets_expiry_idx on result_cache.result_sets(expires_at);
create index if not exists result_sets_lru_idx on result_cache.result_sets(organization_id,last_accessed_at,id);
create unlogged table if not exists result_cache.result_chunks (
 result_id uuid not null,
 organization_id uuid not null,
 chunk_index int not null check (chunk_index >= 0),
 nonce bytea not null,
 ciphertext bytea not null,
 primary key(result_id,chunk_index),
 foreign key(result_id,organization_id) references result_cache.result_sets(id,organization_id) on delete restrict
) with (autovacuum_vacuum_scale_factor=0.02,autovacuum_analyze_scale_factor=0.05,autovacuum_vacuum_cost_delay=0);
create index if not exists result_chunks_org_idx on result_cache.result_chunks(organization_id);
-- Cache rows are not evidence; deletion implements TTL and audited eviction.
do $$
declare rr text := coalesce(nullif(current_setting('portcullis.runtime_role',true),''),'portcullis_runtime');
begin
 if exists(select 1 from pg_roles where rolname=rr) then
  execute format('grant usage on schema result_cache to %I',rr);
  execute format('grant select,insert,update,delete on all tables in schema result_cache to %I',rr);
 end if;
end
$$;
