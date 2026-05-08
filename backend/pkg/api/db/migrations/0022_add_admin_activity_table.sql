-- +migrate Up

-- admin_activity holds activity entries for admin-originated events
-- (e.g. channel package updates). This table is replicated from the primary
-- to subscribers, unlike the runtime 'activity' table which stays local.
create table if not exists admin_activity (
    id uuid primary key default uuid_generate_v4(),
    created_ts timestamptz default current_timestamp not null,
    severity integer not null,
    version varchar(255) not null check (version <> ''),
    application_id uuid not null references application (id) on delete cascade,
    channel_id uuid references channel (id) on delete cascade
);

create index on admin_activity (application_id);
create index on admin_activity (channel_id);

-- Migrate existing channel-package-updated entries (class=6) from activity.
insert into admin_activity (created_ts, severity, version, application_id, channel_id)
select created_ts, severity, version, application_id, channel_id
from activity where class = 6;

delete from activity where class = 6;

-- Unified view combining runtime activity and admin activity.
-- This lets existing queries read from one source without code duplication.
-- admin_activity rows get class=6 and NULL for group_id/instance_id.
create or replace view all_activity as
select
    id::text as id, created_ts, class, severity, version,
    application_id, group_id, channel_id, instance_id
from activity
union all
select
    id::text as id, created_ts, 6 as class, severity, version,
    application_id, null::uuid as group_id, channel_id, null::varchar(50) as instance_id
from admin_activity;

-- +migrate Down

drop view if exists all_activity;

-- Move entries back to activity before dropping.
insert into activity (created_ts, class, severity, version, application_id, channel_id)
select created_ts, 6, severity, version, application_id, channel_id
from admin_activity;

drop table if exists admin_activity;
