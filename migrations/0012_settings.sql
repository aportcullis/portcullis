-- 0012_settings: the runtime settings store (ADR-0017) — org-scoped override
-- rows for the Tier-C operator tunables.
--
-- Which keys exist (and their bounds) is code: the domain descriptor registry
-- (internal/domain/setting) validates every write and read. A row here is an
-- explicit override; an ABSENT row means "use the env seed / compiled
-- default", so reset-to-default is DELETE — see the grant below.

create table if not exists settings (
    organization_id uuid not null references organizations (id) on delete restrict,
    key             text not null check (char_length(key) <= 128),
    -- Values are short canonical texts ("10s", "info", "5"); the descriptor
    -- validates semantics, this CHECK only bounds storage (data.md).
    value           text not null check (char_length(value) <= 256),
    -- Optimistic concurrency for the admin RPC (same shape as connections).
    version         bigint not null default 1 check (version > 0),
    updated_by      uuid not null references users (id) on delete restrict,
    updated_at      timestamptz not null default now(),
    primary key (organization_id, key)
);

-- FK/lookup indexes (data.md): organization_id is the PK prefix; updated_by
-- gets its own.
create index if not exists settings_updated_by_idx on settings (updated_by);

-- NOT sensitive (data.md review gate, explicit judgment): rows are mutable
-- operational overrides, not evidence — the audit trail of every change lives
-- in append-only audit_events (SETTING_UPDATED, same transaction). Reset
-- semantics REQUIRE hard DELETE (absent row = default, ADR-0017), which 0010
-- revoked globally, so per 0010's own rule this migration grants it back for
-- this one table and privcheck registers the matching tablePolicies exception.
do $$
declare
    rr text := coalesce(nullif(current_setting('portcullis.runtime_role', true), ''), 'portcullis_runtime');
begin
    if exists (select 1 from pg_roles where rolname = rr) then
        execute format('grant delete on public.settings to %I', rr);
    end if;
end
$$;

-- Change propagation (ADR-0017): any committed write to settings notifies the
-- 'settings_changed' channel with the key as a signal-only payload; listeners
-- re-read the table (the table is the source of truth, the NOTIFY is the
-- nudge). A trigger keeps the signal attached to the data change no matter
-- which code path writes.
create or replace function settings_notify() returns trigger
language plpgsql as $$
begin
    perform pg_notify('settings_changed', coalesce(new.key, old.key));
    return null;
end
$$;

drop trigger if exists settings_notify_trigger on settings;
create trigger settings_notify_trigger
    after insert or update or delete on settings
    for each row execute function settings_notify();

-- Permission catalog additions (ADR-0008): settings.* joins the seeded
-- catalog, and the system admin role — which received the 0002-era catalog by
-- cross join at seed time — is granted the new keys explicitly here.
insert into permissions (key, description) values
    ('settings.list', 'List runtime settings'),
    ('settings.get', 'View a runtime setting'),
    ('settings.update', 'Change or reset a runtime setting')
on conflict (key) do nothing;

insert into role_permissions (role_id, permission_key)
select rl.id, t.k
from roles rl
join organizations o on o.id = rl.organization_id and o.slug = 'default'
cross join (values ('settings.list'), ('settings.get'), ('settings.update')) as t(k)
where rl.name = 'admin' and rl.is_system
on conflict do nothing;
