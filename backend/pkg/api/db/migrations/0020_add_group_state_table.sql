-- +migrate Up

-- group_state holds runtime-mutable state for groups that needs to be writable
-- by subscriber instances. Only rollout_in_progress is moved here because it's
-- set by the runtime rollout engine (per-region). policy_updates_enabled stays
-- on the groups table because it's admin-controlled and must replicate globally.
create table if not exists group_state (
    group_id uuid primary key references groups (id) on delete cascade,
    rollout_in_progress boolean default false not null,
    safe_mode_disabled boolean default false not null
);

-- Seed group_state from existing groups data.
insert into group_state (group_id, rollout_in_progress, safe_mode_disabled)
select id, rollout_in_progress, false
from groups
on conflict (group_id) do nothing;

-- Drop only rollout_in_progress from groups (policy_updates_enabled stays).
alter table groups drop column rollout_in_progress;

-- +migrate Down

-- Re-add rollout_in_progress to groups.
alter table groups add column rollout_in_progress boolean default false not null;

-- Restore data from group_state.
update groups g set
    rollout_in_progress = gs.rollout_in_progress
from group_state gs
where g.id = gs.group_id;

drop table if exists group_state;
