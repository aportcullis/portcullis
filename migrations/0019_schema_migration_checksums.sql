-- Record the sha256 of each applied migration file so the runner refuses an edited released migration; the runner backfills rows applied before this column existed (ADR-0009).
-- The runner creates the history table before any migration; declaring it here as well lets schema tooling such as sqlc parse this file.
create table if not exists public.schema_migrations (
    version text primary key,
    applied_at timestamptz not null default now());
alter table public.schema_migrations add column if not exists checksum bytea;
