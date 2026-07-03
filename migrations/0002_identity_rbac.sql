-- 0002_identity_rbac: RBAC (permission catalog, roles, role_permissions),
-- external OIDC identities, and switch memberships from a role enum to role_id.
-- The permission catalog and the seeded system roles live here in SQL (the
-- source of truth), loaded at startup; nothing is hardcoded in Go (ADR-0008).

-- Fine-grained permission catalog (Google-IAM style resource.verb).
create table if not exists permissions (
    key          text primary key,
    description  text not null default ''
);

-- Roles: org-scoped bundles of permissions. Seeded system roles are defaults,
-- not a closed set; admins create custom roles. is_bootstrap_default marks the
-- role bootstrap assigns (resolved by flag, never by a hardcoded name).
create table if not exists roles (
    id                   uuid primary key default gen_random_uuid(),
    organization_id      uuid not null references organizations (id) on delete restrict,
    name                 text not null,
    is_system            boolean not null default false,
    is_bootstrap_default boolean not null default false,
    created_at           timestamptz not null default now(),
    deleted_at           timestamptz, -- soft delete; never hard-deleted
    -- FK target for memberships: lets a membership require its role to be in the
    -- same org (a role can't be assigned across organizations).
    unique (id, organization_id)
);
-- Unique role name per org, and a single bootstrap-default per org — both ignore
-- soft-deleted rows, so deleting a role frees its name for reuse.
create unique index if not exists roles_org_name
    on roles (organization_id, name) where deleted_at is null;
create unique index if not exists roles_one_bootstrap_default
    on roles (organization_id) where is_bootstrap_default and deleted_at is null;

create table if not exists role_permissions (
    role_id        uuid not null references roles (id) on delete restrict,
    permission_key text not null references permissions (key) on delete restrict,
    primary key (role_id, permission_key)
);
-- reverse lookups (which roles grant a permission) and FK checks.
create index if not exists role_permissions_permission_idx on role_permissions (permission_key);

-- Switch memberships to role_id. This *assumes* organization_memberships has no
-- rows (true on a fresh install; 0001 is unreleased) — add + NOT NULL would fail
-- on a populated table. If 0001 ever ships with data, replace this with a
-- nullable-add -> backfill -> set-not-null sequence, or fold role_id into 0001.
alter table organization_memberships drop column role;
alter table organization_memberships add column role_id uuid not null;
-- Composite FK: the assigned role must belong to the membership's own org, so a
-- user can never be granted a role from a different organization (ADR-0004).
alter table organization_memberships
    add constraint organization_memberships_role_in_org
    foreign key (role_id, organization_id) references roles (id, organization_id) on delete restrict;
-- index the FK columns: user_id powers permission resolution, role_id powers FK checks.
create index if not exists organization_memberships_user_idx on organization_memberships (user_id);
create index if not exists organization_memberships_role_idx on organization_memberships (role_id);

-- External OIDC identities (e.g. Google), linked to a local user by (issuer, subject).
create table if not exists oidc_identities (
    id          uuid primary key default gen_random_uuid(),
    user_id     uuid not null references users (id) on delete restrict,
    issuer      text not null,
    subject     text not null,
    email       text not null default '',
    created_at  timestamptz not null default now(),
    unique (issuer, subject)
);
create index if not exists oidc_identities_user_idx on oidc_identities (user_id);

-- Seed the permission catalog.
insert into permissions (key, description) values
    ('users.list', 'List users'),
    ('users.get', 'View a user'),
    ('users.create', 'Create a user'),
    ('users.update', 'Update a user'),
    ('users.disable', 'Disable a user'),
    ('roles.list', 'List roles'),
    ('roles.get', 'View a role'),
    ('roles.create', 'Create a role'),
    ('roles.update', 'Update a role'),
    ('roles.delete', 'Delete a role'),
    ('connections.list', 'List connections'),
    ('connections.get', 'View a connection'),
    ('connections.create', 'Create a connection'),
    ('connections.update', 'Update a connection'),
    ('connections.delete', 'Delete a connection'),
    ('connections.test', 'Test a connection'),
    ('policies.get', 'View a connection policy'),
    ('policies.update', 'Update a connection policy'),
    ('requests.list', 'List access requests'),
    ('requests.get', 'View an access request'),
    ('requests.create', 'Create an access request'),
    ('requests.execute', 'Execute an approved request'),
    ('requests.approve', 'Approve an access request'),
    ('requests.reject', 'Reject an access request'),
    ('savedqueries.list', 'List saved queries'),
    ('savedqueries.get', 'View a saved query'),
    ('savedqueries.create', 'Create a saved query'),
    ('savedqueries.update', 'Update a saved query'),
    ('savedqueries.delete', 'Delete a saved query'),
    ('savedqueries.share', 'Share a saved query'),
    ('audit.list', 'List audit events'),
    ('audit.get', 'View an audit event')
on conflict (key) do nothing;

-- Seed the three system roles for the default org (admin is the bootstrap default).
insert into roles (organization_id, name, is_system, is_bootstrap_default)
select o.id, r.name, true, r.boot
from organizations o
cross join (values ('admin', true), ('approver', false), ('requester', false)) as r(name, boot)
where o.slug = 'default'
on conflict (organization_id, name) where deleted_at is null do nothing;

-- admin: every permission.
insert into role_permissions (role_id, permission_key)
select rl.id, p.key
from roles rl
join organizations o on o.id = rl.organization_id and o.slug = 'default'
cross join permissions p
where rl.name = 'admin'
on conflict do nothing;

-- approver: requester capabilities + review + audit view.
insert into role_permissions (role_id, permission_key)
select rl.id, t.k
from roles rl
join organizations o on o.id = rl.organization_id and o.slug = 'default'
cross join (values
    ('requests.list'), ('requests.get'), ('requests.create'), ('requests.execute'),
    ('requests.approve'), ('requests.reject'),
    ('savedqueries.list'), ('savedqueries.get'), ('savedqueries.create'),
    ('savedqueries.update'), ('savedqueries.delete'), ('savedqueries.share'),
    ('audit.list'), ('audit.get')
) as t(k)
where rl.name = 'approver'
on conflict do nothing;

-- requester: own requests and saved queries.
insert into role_permissions (role_id, permission_key)
select rl.id, t.k
from roles rl
join organizations o on o.id = rl.organization_id and o.slug = 'default'
cross join (values
    ('requests.list'), ('requests.get'), ('requests.create'), ('requests.execute'),
    ('savedqueries.list'), ('savedqueries.get'), ('savedqueries.create'),
    ('savedqueries.update'), ('savedqueries.delete')
) as t(k)
where rl.name = 'requester'
on conflict do nothing;
