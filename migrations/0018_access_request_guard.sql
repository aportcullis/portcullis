-- Enforce the request state graph and submit-snapshot immutability below the application (ADR-0018).
-- The guard bounds principals that cannot disable it: a role holding the table owner's privileges could drop the trigger, so it is exempt and runtime boot verification forbids the runtime role that reach (ADR-0009).
create or replace function public.guard_access_request_lifecycle() returns trigger language plpgsql as $$
declare
    frozen public.access_requests;
begin
    if pg_has_role(current_user, (select relowner from pg_catalog.pg_class where oid = tg_relid), 'USAGE') then
        return new;
    end if;

    if new.id is distinct from old.id or new.organization_id is distinct from old.organization_id
       or new.connection_id is distinct from old.connection_id or new.requester_id is distinct from old.requester_id
       or new.created_at is distinct from old.created_at then
        raise exception 'access request identity is immutable' using errcode = '42501';
    end if;

    -- Terminal rows accept only a key-rotation rewrap of the sealed payload envelope; the payload digest keeps authenticating the content.
    if old.state in ('rejected', 'expired', 'cancelled', 'succeeded', 'failed', 'outcome_unknown') then
        frozen := new;
        frozen.payload_key_version := old.payload_key_version;
        frozen.payload_wrapped_dek := old.payload_wrapped_dek;
        frozen.payload_nonce := old.payload_nonce;
        frozen.payload_ciphertext := old.payload_ciphertext;
        if frozen is distinct from old then
            raise exception 'terminal access request is immutable' using errcode = '42501';
        end if;
        return new;
    end if;

    -- Mirror of access.State.CanTransitionTo; the domain pin test walks every pair against this guard.
    if new.state is distinct from old.state and (old.state, new.state) not in (
        ('draft', 'pending'), ('draft', 'approved'), ('draft', 'cancelled'),
        ('pending', 'approved'), ('pending', 'rejected'), ('pending', 'cancelled'), ('pending', 'expired'),
        ('approved', 'executing'), ('approved', 'cancelled'), ('approved', 'expired'),
        ('executing', 'succeeded'), ('executing', 'failed'), ('executing', 'cancelled'), ('executing', 'outcome_unknown')) then
        raise exception 'access request transition % -> % is not allowed', old.state, new.state using errcode = '42501';
    end if;

    if new.state = 'draft' and new.submitted_at is not null then
        raise exception 'a draft carries no submit snapshot' using errcode = '42501';
    end if;

    -- After submission only lifecycle bookkeeping and the rotatable envelope may change.
    if old.submitted_at is not null then
        frozen := new;
        frozen.state := old.state;
        frozen.state_reason := old.state_reason;
        frozen.expires_at := old.expires_at;
        frozen.version := old.version;
        frozen.updated_at := old.updated_at;
        frozen.payload_key_version := old.payload_key_version;
        frozen.payload_wrapped_dek := old.payload_wrapped_dek;
        frozen.payload_nonce := old.payload_nonce;
        frozen.payload_ciphertext := old.payload_ciphertext;
        if frozen is distinct from old then
            raise exception 'access request submit snapshot is immutable' using errcode = '42501';
        end if;
    end if;

    if new.expires_at is distinct from old.expires_at
       and not (new.state = 'approved' and old.state in ('draft', 'pending')) then
        raise exception 'approval expiry is set only by the approving transition' using errcode = '42501';
    end if;

    if new.state_reason is distinct from old.state_reason and new.state = old.state then
        raise exception 'a state reason changes only with its transition' using errcode = '42501';
    end if;

    return new;
end
$$;

drop trigger if exists access_requests_lifecycle on public.access_requests;
create trigger access_requests_lifecycle before update on public.access_requests
for each row execute function public.guard_access_request_lifecycle();
