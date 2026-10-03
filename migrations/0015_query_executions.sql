-- Execution evidence is retained; one request can acquire only one attempt.
create table if not exists public.query_executions (
    request_id uuid primary key,
    organization_id uuid not null,
    owner text not null check (length(owner) between 1 and 128),
    attempt_id uuid not null unique,
    heartbeat timestamptz not null,
    deadline timestamptz not null,
    started_at timestamptz not null,
    finished_at timestamptz,
    outcome text check (outcome in ('succeeded', 'failed', 'cancelled', 'outcome_unknown')),
    rows_affected bigint not null default 0,
    duration_ms bigint not null default 0 check (duration_ms >= 0),
    result_id uuid,
    result_expires_at timestamptz,
    row_count bigint not null default 0 check (row_count >= 0),
    byte_count bigint not null default 0 check (byte_count >= 0),
    truncated boolean not null default false,
    foreign key (request_id, organization_id) references public.access_requests (id, organization_id) on delete restrict,
    check ((outcome is null) = (finished_at is null)),
    check ((result_id is null) = (result_expires_at is null))
);
create index if not exists query_executions_org_deadline_idx
    on public.query_executions (organization_id, deadline) where outcome is null;
-- Runtime may update lease and completion metadata, but never delete execution history.
-- Completed execution evidence and attempt identity are immutable.
create or replace function public.guard_execution_evidence() returns trigger language plpgsql as $$
begin
 if old.outcome is not null or new.request_id <> old.request_id or new.organization_id <> old.organization_id
    or new.owner <> old.owner or new.attempt_id <> old.attempt_id or new.started_at <> old.started_at then
  raise exception 'execution evidence is immutable' using errcode='42501';
 end if;
 return new;
end
$$;
drop trigger if exists query_executions_evidence on public.query_executions;
create trigger query_executions_evidence before update on public.query_executions
for each row execute function public.guard_execution_evidence();
