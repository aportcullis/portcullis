-- Atomically retire single-role authority after grant/recovery equivalence checks (ADR-0062/0064).
lock table public.organizations in share mode;
do $$
declare organization record; hash_value bigint; encoded bytea; byte_index integer;
begin
    -- Match the application's FNV-1a organization lock, before any policy or workflow row locks.
    for organization in select id from public.organizations order by id loop
        hash_value := 2166136261;
        encoded := convert_to(organization.id::text, 'UTF8');
        for byte_index in 0..length(encoded)-1 loop
            hash_value := mod((hash_value # get_byte(encoded, byte_index)::bigint) * 16777619, 4294967296);
        end loop;
        if hash_value >= 2147483648 then hash_value := hash_value - 4294967296; end if;
        perform pg_catalog.pg_advisory_xact_lock(5, hash_value::integer);
    end loop;
end $$;
lock table public.organization_memberships, public.roles, public.role_permissions, public.users,
    public.role_bindings, public.role_binding_versions, public.organization_authorization_revisions in access exclusive mode;

do $$
begin
    if exists(select 1 from public.role_bindings) or exists(select 1 from public.role_binding_versions)
       or exists(select 1 from public.organization_authorization_revisions) then
        raise exception 'binding activation refused: prepared policy storage is not empty';
    end if;
end $$;

create temporary table binding_activation_mapping on commit drop as
select member.id as membership_id, member.organization_id, member.user_id, member.role_id,
       gen_random_uuid() as binding_id
from public.organization_memberships member;
create temporary table binding_activation_grants on commit drop as
select member.organization_id, member.user_id, permission.permission_key
from public.organization_memberships member
join public.roles role on role.id=member.role_id and role.organization_id=member.organization_id and role.deleted_at is null
join public.role_permissions permission on permission.role_id=role.id and permission.deleted_at is null;

insert into public.permissions(key,description) values
 ('bindings.list','List role bindings'),('bindings.get','Read role binding details'),
 ('bindings.create','Create role bindings'),('bindings.update','Update role bindings'),('bindings.delete','Revoke role bindings');
-- Only the explicitly seeded system bootstrap role gains the newly introduced operations.
insert into public.role_permissions(role_id,permission_key)
select role.id,permission.key from public.roles role cross join public.permissions permission
where role.is_system and role.is_bootstrap_default and role.deleted_at is null and permission.key like 'bindings.%';

insert into public.role_bindings(id,organization_id,user_id,current_version,created_at)
select binding_id,organization_id,user_id,1,clock_timestamp() from binding_activation_mapping;
insert into public.role_binding_versions(binding_id,organization_id,user_id,version,role_id,scope_type,created_at)
select binding.id,binding.organization_id,binding.user_id,1,mapping.role_id,'organization',binding.created_at
from public.role_bindings binding join binding_activation_mapping mapping on mapping.binding_id=binding.id;
insert into public.organization_authorization_revisions(organization_id,revision)
select id,1 from public.organizations;

do $$
begin
    if (select count(*) from public.role_bindings)<>(select count(*) from binding_activation_mapping)
       or exists(select 1 from binding_activation_mapping mapping left join public.role_binding_versions version
         on version.binding_id=mapping.binding_id and version.organization_id=mapping.organization_id
         and version.user_id=mapping.user_id and version.role_id=mapping.role_id and version.version=1
         where version.binding_id is null) then
        raise exception 'binding activation refused: incomplete membership mapping';
    end if;
    if exists(
       (select * from binding_activation_grants except
        select binding.organization_id,binding.user_id,permission.permission_key
        from public.role_bindings binding join public.role_binding_versions version on version.binding_id=binding.id and version.version=binding.current_version
        join public.roles role on role.id=version.role_id and role.organization_id=binding.organization_id and role.deleted_at is null
        join public.role_permissions permission on permission.role_id=role.id and permission.deleted_at is null
        where permission.permission_key not like 'bindings.%')
       union all
       (select binding.organization_id,binding.user_id,permission.permission_key
        from public.role_bindings binding join public.role_binding_versions version on version.binding_id=binding.id and version.version=binding.current_version
        join public.roles role on role.id=version.role_id and role.organization_id=binding.organization_id and role.deleted_at is null
        join public.role_permissions permission on permission.role_id=role.id and permission.deleted_at is null
        where permission.permission_key not like 'bindings.%'
        except select * from binding_activation_grants)
    ) then raise exception 'binding activation refused: effective grants differ'; end if;
    if exists(select 1 from public.organizations organization
      where exists(select 1 from public.organization_memberships member where member.organization_id=organization.id)
      and not exists(
       select 1 from public.organization_memberships member join public.users account on account.id=member.user_id and account.status='active'
       where member.organization_id=organization.id and not exists(
        select 1 from public.permissions required
        where (required.key like 'users.%' or required.key like 'roles.%' or required.key like 'bindings.%')
        and not exists(
         select 1 from public.role_bindings binding
         join public.role_binding_versions version on version.binding_id=binding.id and version.version=binding.current_version
         join public.roles role on role.id=version.role_id and role.organization_id=binding.organization_id and role.deleted_at is null
         join public.role_permissions permission on permission.role_id=role.id and permission.deleted_at is null
         where binding.organization_id=organization.id and binding.user_id=member.user_id
           and version.scope_type='organization' and version.condition_expression is null and version.revoked_at is null
           and permission.permission_key=required.key
        )
       )
      )
    ) then raise exception 'binding activation refused: no recovery administrator'; end if;
end $$;

-- Seal the retired assignment slot. Keeping an always-NULL tombstone avoids a
-- destructive column removal while making old assignment writers fail closed.
alter table public.organization_memberships alter column role_id drop not null;
update public.organization_memberships set role_id=null;
alter table public.organization_memberships add constraint membership_legacy_role_retired check (role_id is null);
do $$
declare runtime_role text:=coalesce(nullif(current_setting('portcullis.runtime_role',true),''),'portcullis_runtime');
begin
    execute format('grant select,insert,update on public.role_bindings to %I',runtime_role);
    execute format('grant select,insert on public.role_binding_versions to %I',runtime_role);
    execute format('grant select,update on public.organization_authorization_revisions to %I',runtime_role);
    execute format('revoke insert on public.organization_authorization_revisions from %I',runtime_role);
    execute format('revoke delete,truncate,trigger,references,maintain on public.role_bindings,public.organization_authorization_revisions from %I',runtime_role);
    execute format('revoke update,delete,truncate,trigger,references,maintain on public.role_binding_versions from %I',runtime_role);
end $$;
