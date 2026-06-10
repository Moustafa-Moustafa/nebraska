-- +migrate Up

create table group_state (
	group_id uuid primary key references groups (id) on delete cascade,
	rollout_in_progress boolean default false not null,
	updates_disabled_due_to_failure boolean default false not null,
	created_ts timestamptz default current_timestamp not null
);

insert into group_state (group_id, rollout_in_progress, created_ts)
select id, rollout_in_progress, created_ts from groups;

-- +migrate StatementBegin
create or replace function create_group_state_for_group() returns trigger as $$
begin
	insert into public.group_state (group_id) values (new.id)
	on conflict (group_id) do nothing;
	return new;
end;
$$ language plpgsql;
-- +migrate StatementEnd

create trigger groups_create_group_state
after insert on groups
for each row execute function create_group_state_for_group();

alter table groups enable always trigger groups_create_group_state;

alter table groups drop column rollout_in_progress;

alter table groups
	add column force_updates_enabled_ts timestamptz;

-- +migrate StatementBegin
create or replace function clear_updates_disabled_on_force_enable() returns trigger as $$
begin
	update public.group_state
	set updates_disabled_due_to_failure = false
	where group_id = new.id
	  and updates_disabled_due_to_failure;
	return new;
end;
$$ language plpgsql;
-- +migrate StatementEnd

create trigger groups_clear_updates_disabled_on_force_enable
after update on groups
for each row
when (new.force_updates_enabled_ts is distinct from old.force_updates_enabled_ts)
execute function clear_updates_disabled_on_force_enable();

alter table groups enable always trigger groups_clear_updates_disabled_on_force_enable;

-- +migrate Down

drop trigger if exists groups_clear_updates_disabled_on_force_enable on groups;
drop function if exists clear_updates_disabled_on_force_enable();

alter table groups
	drop column force_updates_enabled_ts;

alter table groups add column rollout_in_progress boolean default false not null;

update groups g
set rollout_in_progress = gs.rollout_in_progress
from group_state gs
where gs.group_id = g.id;

drop trigger if exists groups_create_group_state on groups;
drop function if exists create_group_state_for_group();
drop table group_state;
