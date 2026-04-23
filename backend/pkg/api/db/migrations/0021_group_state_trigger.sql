-- +migrate Up

-- Automatically create a group_state row when a group is inserted.
-- On the primary, the trigger creates a default row with rollout_in_progress=false
-- and safe_mode_disabled=false. On subscribers, groups arrive via logical
-- replication and the trigger creates group_state with the same defaults.
-- ENABLE ALWAYS ensures the trigger fires regardless of session_replication_role
-- (including replica mode used by the logical replication apply worker).
-- Note: the trigger function must use schema-qualified table names
-- (public.group_state) because the search_path may differ during
-- replication apply.

-- +migrate StatementBegin
create or replace function create_group_state_on_insert()
returns trigger as $$
begin
    insert into public.group_state (group_id, rollout_in_progress, safe_mode_disabled)
    values (NEW.id, false, false)
    on conflict (group_id) do nothing;
    return NEW;
end;
$$ language plpgsql;
-- +migrate StatementEnd

create trigger trg_create_group_state
    after insert on groups
    for each row
    execute function create_group_state_on_insert();

-- Fire on all instances regardless of session_replication_role
alter table groups enable always trigger trg_create_group_state;

-- +migrate Down

drop trigger if exists trg_create_group_state on groups;
drop function if exists create_group_state_on_insert();
