package dbreads

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/doug-martin/goqu/v9"
	"golang.org/x/sync/errgroup"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/types"
)

// --- duration helpers ----------------------------------------------------
// These are the internal types/consts/helpers used to translate the duration
// param strings ("1h", "1d", "7d", "30d") used by the group-stats reads into
// Postgres INTERVAL literals.

type (
	durationParam    string
	durationCode     int
	postgresInterval string
)

const (
	oneHour durationCode = iota
	oneDay
	sevenDays
	thirtyDays
)

const (
	// deadInstanceTimeSpan is the cutoff past which an instance is treated
	// as dead in the version timeline queries.
	deadInstanceTimeSpan = "6 months"
)

var durationParamToCode = map[durationParam]durationCode{
	"1h":  oneHour,
	"1d":  oneDay,
	"7d":  sevenDays,
	"30d": thirtyDays,
}

func durationCodeToPostgresTimings(code durationCode) (shared.PostgresDuration, postgresInterval, error) {
	switch code {
	case thirtyDays:
		return "30 days", "3 days", nil
	case sevenDays:
		return "7 days", "1 days", nil
	case oneDay:
		return "1days", "1 hour", nil
	case oneHour:
		return "1hour", "15 minute", nil
	default:
		return "", "", fmt.Errorf("invalid duration enumeration value %d", code)
	}
}

func durationParamToPostgresTimings(duration durationParam) (shared.PostgresDuration, postgresInterval, error) {
	code, ok := durationParamToCode[duration]
	if !ok {
		return "", "", fmt.Errorf("invalid duration param %s", duration)
	}
	return durationCodeToPostgresTimings(code)
}

// isNightlyVersion returns whether a version is a nightly build (not counted
// in the version-count timelines).
func isNightlyVersion(version string) bool {
	return strings.Contains(version, "nightly")
}

func updateVersionTimeline(timeline map[time.Time]types.VersionCountMap, spans []time.Time, from time.Time, to time.Time, version string) {
	if isNightlyVersion(version) {
		return
	}
	for _, span := range spans {
		if span.After(from) && span.Before(to) {
			timeline[span][version]++
		} else {
			if _, ok := timeline[span][version]; !ok {
				timeline[span][version] = 0
			}
		}
	}
}

// --- caches --------------------------------------------------------------
// cachedGroups maps GroupDescriptor -> group ID for GetGroupID. Invalidated
// whenever a group is added/updated/deleted by an admin writer in pkg/api.
//
// cachedGroupVersionCount is a small TTL cache for GetGroupVersionCountTimeline.
// SetCacheLifespanForTest lets tests shrink the TTL for fast invalidation.

type groupDurationCacheKey struct {
	GroupID  string
	Duration string
}

type groupVersionCountCache struct {
	data     map[time.Time](types.VersionCountMap)
	storedAt time.Time
}

var (
	cachedGroups                    map[types.GroupDescriptor]string
	cachedGroupsLock                sync.RWMutex
	cachedGroupVersionCount         = make(map[groupDurationCacheKey]groupVersionCountCache)
	cachedGroupVersionCountLock     sync.RWMutex
	cachedGroupVersionCountLifespan = time.Minute
)

// InvalidateCachedGroups clears cachedGroups so the next GetGroupID rebuilds
// it from the DB. Called by admin group writers after Add/Update/Delete.
func InvalidateCachedGroups() {
	cachedGroupsLock.Lock()
	cachedGroups = nil
	cachedGroupsLock.Unlock()
}

// SetCacheLifespanForTest sets the TTL used by GetGroupVersionCountTimeline's
// in-memory cache and returns the previous value so tests can restore it.
func SetCacheLifespanForTest(lifespan time.Duration) time.Duration {
	cachedGroupVersionCountLock.Lock()
	prev := cachedGroupVersionCountLifespan
	cachedGroupVersionCountLifespan = lifespan
	cachedGroupVersionCountLock.Unlock()
	return prev
}

// --- reads ---------------------------------------------------------------

// GetGroup returns the group identified by the id provided. The Channel
// field is hydrated when set.
func (q *Queries) GetGroup(groupID string) (*types.Group, error) {
	var group types.Group

	query, _, err := q.groupsQuery().
		Where(goqu.I("groups.id").Eq(groupID)).
		ToSQL()
	if err != nil {
		return nil, err
	}
	if err := q.db.QueryRowx(query).StructScan(&group); err != nil {
		return nil, err
	}
	if group.ChannelID.String == "" {
		group.Channel = nil
	} else {
		channel, err := q.GetChannel(group.ChannelID.String)
		switch err {
		case nil:
			group.Channel = channel
		case sql.ErrNoRows:
			group.Channel = nil
		default:
			return nil, err
		}
	}
	return &group, nil
}

// GetGroupID returns the ID of the first group matching the given track name
// and channel architecture for the given app. Backed by an in-memory cache
// invalidated by InvalidateCachedGroups.
func (q *Queries) GetGroupID(appID, trackName string, arch types.Arch) (string, error) {
	var cachedGroupsRef map[types.GroupDescriptor]string
	cachedGroupsLock.RLock()
	if cachedGroups != nil {
		cachedGroupsRef = cachedGroups
	}
	cachedGroupsLock.RUnlock()

	if cachedGroupsRef == nil {
		cachedGroupsLock.Lock()
		cachedGroupsRef = cachedGroups
		if cachedGroupsRef == nil {
			cachedGroups = make(map[types.GroupDescriptor]string)
			query, _, err := goqu.From("groups").ToSQL()
			var groups []*types.Group
			if err == nil {
				groups, err = q.getGroupsFromQuery(query)
			}
			if err != nil {
				l.Error().Err(err).Msg("GetGroupID error")
			} else {
				for _, group := range groups {
					if group.Channel != nil {
						descriptor := types.GroupDescriptor{AppID: group.ApplicationID, Track: group.Track, Arch: group.Channel.Arch}
						if otherID, ok := cachedGroups[descriptor]; ok {
							l.Warn().Str("group", group.ID).Str("group2", otherID).Str("track", group.Track).Msg("GetGroupID - another group already uses the same track name and architecture")
						}
						cachedGroups[descriptor] = group.ID
					} else {
						l.Warn().Str("group", group.ID).Msg("GetGroupID - no channel found for")
					}
				}
			}
			cachedGroupsRef = cachedGroups
		}
		cachedGroupsLock.Unlock()
	}

	appIDNoBrackets := strings.TrimSpace(appID)
	if len(appIDNoBrackets) > 1 && appIDNoBrackets[0] == '{' {
		appIDNoBrackets = strings.TrimSpace(appIDNoBrackets[1 : len(appIDNoBrackets)-1])
	}

	cachedGroupID, ok := cachedGroupsRef[types.GroupDescriptor{AppID: appIDNoBrackets, Track: trackName, Arch: arch}]
	if !ok {
		return "", fmt.Errorf("no group found for app %v, track %v, and architecture %v", appID, trackName, arch)
	}
	return cachedGroupID, nil
}

// GetGroupsCount returns the total number of groups in an app.
func (q *Queries) GetGroupsCount(appID string) (int, error) {
	query := goqu.From("groups").
		Where(goqu.C("application_id").Eq(appID)).
		Select(goqu.L("count(*)"))
	return q.GetCountQuery(query)
}

// GetGroups returns a paginated list of groups for the given app.
func (q *Queries) GetGroups(appID string, page, perPage uint64) ([]*types.Group, error) {
	page, perPage = shared.ValidatePaginationParams(page, perPage)
	limit, offset := shared.SQLPaginate(page, perPage)
	query, _, err := q.groupsQuery().Where(goqu.C("application_id").Eq(appID)).
		Limit(limit).
		Offset(offset).
		ToSQL()
	if err != nil {
		return nil, err
	}
	return q.getGroupsFromQuery(query)
}

// GetGroupsForApp returns every group for the given app. Used internally by
// application reads to hydrate Application.Groups. Renamed from the previous
// private getGroups so callers across the package boundary can reach it.
func (q *Queries) GetGroupsForApp(appID string) ([]*types.Group, error) {
	query, _, err := q.groupsQuery().Where(goqu.C("application_id").Eq(appID)).ToSQL()
	if err != nil {
		return nil, err
	}
	return q.getGroupsFromQuery(query)
}

func (q *Queries) getGroupsFromQuery(query string) ([]*types.Group, error) {
	var groups []*types.Group
	rows, err := q.db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		group := types.Group{}
		if err := rows.StructScan(&group); err != nil {
			return nil, err
		}
		if group.ChannelID.String == "" {
			group.Channel = nil
		} else {
			channel, err := q.GetChannel(group.ChannelID.String)
			switch err {
			case nil:
				group.Channel = channel
			case sql.ErrNoRows:
				group.Channel = nil
			default:
				return nil, err
			}
		}
		groups = append(groups, &group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

// GetGroupUpdatesStats returns the distribution of update status for the
// instances in the given group. Renamed from the previous private
// getGroupUpdatesStats so runtime writers in pkg/api/updates.go and
// pkg/api/events.go can reach it across the package boundary.
func (q *Queries) GetGroupUpdatesStats(group *types.Group) (*types.UpdatesStats, error) {
	var updatesStats types.UpdatesStats

	packageVersion := ""
	if group.Channel != nil && group.Channel.Package != nil {
		packageVersion = group.Channel.Package.Version
	}
	query, _, err := goqu.From("instance_application").Select(
		goqu.COUNT("*").As("total_instances"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when last_update_version = ? then 1 else 0 end", packageVersion)), 0).As("updates_to_current_version_granted"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when update_in_progress = 'false' and last_update_version = ? then 1 else 0 end", packageVersion)), 0).As("updates_to_current_version_attempted"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when update_in_progress = 'false' and last_update_version = ? and last_update_version = version then 1 else 0 end", packageVersion)), 0).As("updates_to_current_version_succeeded"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when update_in_progress = 'false' and last_update_version = ? and last_update_version != version then 1 else 0 end", packageVersion)), 0).As("updates_to_current_version_failed"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when last_update_granted_ts > now() at time zone 'utc' - interval ? then 1 else 0 end", group.PolicyPeriodInterval)), 0).As("updates_granted_in_last_period"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when update_in_progress = 'true' and now() at time zone 'utc' - last_update_granted_ts <= interval ? then 1 else 0 end", group.PolicyUpdateTimeout)), 0).As("updates_in_progress"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when update_in_progress = 'true' and now() at time zone 'utc' - last_update_granted_ts > interval ? then 1 else 0 end", group.PolicyUpdateTimeout)), 0).As("updates_timed_out"),
	).Where(goqu.C("group_id").Eq(group.ID),
		goqu.L("last_check_for_updates > now() at time zone 'utc' - interval ?", shared.ValidityInterval),
		goqu.L(shared.IgnoreFakeInstanceCondition("instance_id")),
	).ToSQL()
	if err != nil {
		return nil, err
	}
	if err := q.db.QueryRowx(query).StructScan(&updatesStats); err != nil {
		return nil, err
	}
	return &updatesStats, nil
}

// groupsQuery returns the base SELECT for groups, joining the node-local
// group_local sidecar so the effective policy values reflect the COALESCE of
// (group_local override, groups default). Safe INNER JOIN because the AFTER
// INSERT trigger on groups guarantees a matching group_local row.
func (q *Queries) groupsQuery() *goqu.SelectDataset {
	eff := func(name string) interface{} {
		return goqu.COALESCE(goqu.I("group_local."+name+"_override"), goqu.I("groups."+name)).As(name)
	}
	return goqu.From("groups").
		InnerJoin(
			goqu.T("group_local"),
			goqu.On(goqu.I("groups.id").Eq(goqu.I("group_local.group_id"))),
		).
		Select(
			goqu.I("groups.id"),
			goqu.I("groups.name"),
			goqu.I("groups.description"),
			goqu.I("groups.created_ts"),
			goqu.I("groups.application_id"),
			goqu.I("groups.channel_id"),
			goqu.I("groups.track"),
			goqu.I("group_local.rollout_in_progress"),
			eff("policy_updates_enabled"),
			eff("policy_safe_mode"),
			eff("policy_office_hours"),
			eff("policy_timezone"),
			eff("policy_period_interval"),
			eff("policy_max_updates_per_period"),
			eff("policy_update_timeout"),
		).
		Order(goqu.I("groups.created_ts").Desc())
}

// GetGroupVersionBreakdown returns the version breakdown of all alive
// instances in the given group.
func (q *Queries) GetGroupVersionBreakdown(groupID string) ([]*types.VersionBreakdownEntry, error) {
	var entryList []*types.VersionBreakdownEntry

	semverExpr, err := semverToIntArray("version")
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
	SELECT version, count(*) as instances, (count(*) * 100.0 / total) as percentage
	FROM instance_application, (
		SELECT count(*) as total
		FROM instance_application
		WHERE group_id=$1 AND last_check_for_updates > now() at time zone 'utc' - interval '%[1]s'
		) totals
	WHERE group_id=$1 AND last_check_for_updates > now() at time zone 'utc' - interval '%[1]s' AND %[2]s
	GROUP BY version, total
	ORDER BY %[3]s DESC
	`, shared.ValidityInterval, shared.IgnoreFakeInstanceCondition("instance_id"), semverExpr)
	rows, err := q.db.Queryx(query, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry types.VersionBreakdownEntry
		if err := rows.StructScan(&entry); err != nil {
			return nil, err
		}
		entryList = append(entryList, &entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entryList, nil
}

// GetGroupInstancesStats returns a summary of the status of the instances in
// the given group over the given duration window.
func (q *Queries) GetGroupInstancesStats(groupID, duration string) (*types.InstancesStatusStats, error) {
	var instancesStats types.InstancesStatusStats
	durationString, _, err := durationParamToPostgresTimings(durationParam(duration))
	if err != nil {
		return nil, err
	}

	group, err := q.GetGroup(groupID)
	if err != nil {
		return nil, err
	}

	packageVersion := ""
	if group.Channel != nil && group.Channel.Package != nil {
		packageVersion = group.Channel.Package.Version
	}

	undefinedExpr := goqu.L("case when status IS NULL then 1 else 0 end")
	completedExpr := goqu.L("case when status = ? then 1 else 0 end", types.InstanceStatusComplete)
	if packageVersion != "" {
		undefinedExpr = goqu.L("case when version != ? and status IS NULL then 1 else 0 end", packageVersion)
		completedExpr = goqu.L("case when (version = ? and status IS NULL) or (status = ?) then 1 else 0 end", packageVersion, types.InstanceStatusComplete)
	}

	query, _, err := goqu.From("instance_application").Select(
		goqu.COUNT("*").As("total"),
		goqu.COALESCE(goqu.SUM(undefinedExpr), 0).As("undefined"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when status = ? then 1 else 0 end", types.InstanceStatusError)), 0).As("error"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when status = ? then 1 else 0 end", types.InstanceStatusUpdateGranted)), 0).As("update_granted"),
		goqu.COALESCE(goqu.SUM(completedExpr), 0).As("complete"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when status = ? then 1 else 0 end", types.InstanceStatusInstalled)), 0).As("installed"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when status = ? then 1 else 0 end", types.InstanceStatusDownloaded)), 0).As("downloaded"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when status = ? then 1 else 0 end", types.InstanceStatusDownloading)), 0).As("downloading"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when status = ? then 1 else 0 end", types.InstanceStatusOnHold)), 0).As("onhold"),
	).Where(goqu.C("group_id").Eq(groupID),
		goqu.L("last_check_for_updates > now() at time zone 'utc' - interval ?", durationString),
		goqu.L(shared.IgnoreFakeInstanceCondition("instance_id")),
	).ToSQL()
	if err != nil {
		return nil, err
	}
	if err := q.db.QueryRowx(query).StructScan(&instancesStats); err != nil {
		return nil, err
	}
	return &instancesStats, nil
}

// GetGroupVersionCountTimeline computes the instance version count timeline
// for the given group/duration. Backed by a TTL cache; the second return
// value is true on cache hit.
func (q *Queries) GetGroupVersionCountTimeline(groupID string, duration string) (map[time.Time](types.VersionCountMap), bool, error) {
	cacheKey := groupDurationCacheKey{GroupID: groupID, Duration: duration}

	cachedGroupVersionCountLock.RLock()
	val, ok := cachedGroupVersionCount[cacheKey]
	cachedGroupVersionCountLock.RUnlock()
	if ok {
		if time.Since(val.storedAt) < cachedGroupVersionCountLifespan {
			l.Debug().Str("cacheStatus", "HIT").Str("groupID", groupID).Str("duration", duration).Msg("GetGroupVersionCountTimeline")
			return val.data, true, nil
		}
		l.Debug().Str("cacheStatus", "STALE").Str("groupID", groupID).Str("duration", duration).Msg("GetGroupVersionCountTimeline")
	}

	durationString, interval, err := durationParamToPostgresTimings(durationParam(duration))
	if err != nil {
		return nil, false, err
	}

	queryWg := new(errgroup.Group)

	timelineCount := make(map[time.Time]types.VersionCountMap)
	timelineEntryEntities := []types.VersionCountTimelineEntry{}

	queryWg.Go(func() error {
		instancesWithoutStatusQuery := fmt.Sprintf(`with time_series as ( select * from generate_series( now() - interval '%[2]s', now(), interval '%[3]s' ) as ts ), instances as ( select ia.instance_id, case when last_update_granted_ts is not null then last_update_granted_ts else ia.created_ts end, ia."version" from instance_application ia left join ( select * from instance_status_history where group_id = '%[1]s' and created_ts >= now() - interval '%[4]s' and status = 4 ) ish on ia.instance_id = ish.instance_id where (ia."group_id" = '%[1]s') and last_check_for_updates >= now() - interval '%[2]s' and ( ia.instance_id is null or ia.instance_id not like '{________-____-____-____-____________}' ) and ia.version not like '%%nightly%%' and ish.instance_id is null ) select ts, ( case when version is null then '' else version end ), sum( case when version is not null then 1 else 0 end ) total from ( select * from time_series left join ( select * from instances ) _ on created_ts <= time_series.ts ) as _ group by 1, 2 order by ts desc;`, groupID, durationString, interval, deadInstanceTimeSpan)

		rows, err := q.db.Queryx(instancesWithoutStatusQuery)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var timelineEntryEntity types.VersionCountTimelineEntry
			if err := rows.StructScan(&timelineEntryEntity); err != nil {
				return err
			}
			timelineEntryEntities = append(timelineEntryEntities, timelineEntryEntity)
		}
		return nil
	})

	instanceWithStatusInInterval := []types.InstanceStatusHistoryEntry{}
	queryWg.Go(func() error {
		instanceWithStatusHistoryInInterval := fmt.Sprintf(`
			select * from instance_status_history ish inner join (select instance_id from instance_application where ( "group_id" = '%[1]s' ) AND last_check_for_updates >= now() - interval '%[2]s' AND ( instance_id IS NULL OR instance_id NOT LIKE '{________-____-____-____-____________}')) ia on ish.instance_id = ia.instance_id  where ish.group_id = '%[1]s' and ish.status=4 and ish.created_ts >= now()-interval '%[2]s' order by ish.instance_id,ish.created_ts desc;
			`, groupID, durationString)

		statusHistoryRows, err := q.db.Queryx(instanceWithStatusHistoryInInterval)
		if err != nil {
			return err
		}
		defer statusHistoryRows.Close()
		for statusHistoryRows.Next() {
			rI := types.InstanceStatusHistoryEntry{}
			if err := statusHistoryRows.StructScan(&rI); err != nil {
				return err
			}
			instanceWithStatusInInterval = append(instanceWithStatusInInterval, rI)
		}
		return nil
	})

	type versionCount struct {
		Version string `db:"version"`
		Count   int    `db:"count"`
	}

	versionCounts := []versionCount{}
	queryWg.Go(func() error {
		instancesWithoutStatusInIntervalQuery := fmt.Sprintf(
			`with active_instance as (select instance_id from instance_application where group_id = '%[1]s' and last_check_for_updates >= now() - interval '%[2]s'  and( instance_id IS NULL OR instance_id NOT LIKE '{________-____-____-____-____________}')) ,
			instance_status_interval as (select distinct instance_id from instance_status_history where instance_id in (select instance_id from active_instance) and status = 4 and created_ts >= now()-interval '%[2]s'),
			instance_status_to_process as (select ai.instance_id from active_instance ai left join instance_status_interval isi  on ai.instance_id = isi.instance_id where isi.instance_id is null)
			select version as version, count(*) as count from (select distinct on (instance_id) instance_id,id,status,version,created_ts,application_id,group_id from instance_status_history where instance_id in (select instance_id from instance_status_to_process) and status=4 and created_ts >= now()-interval '%[3]s' order by instance_id,created_ts desc)_ group by version;`,
			groupID, durationString, deadInstanceTimeSpan)

		versionAggRows, err := q.db.Queryx(instancesWithoutStatusInIntervalQuery)
		if err != nil {
			return err
		}
		for versionAggRows.Next() {
			vc := versionCount{}
			if err := versionAggRows.StructScan(&vc); err != nil {
				return err
			}
			versionCounts = append(versionCounts, vc)
		}
		return nil
	})

	if err := queryWg.Wait(); err != nil {
		return nil, false, err
	}

	allVersions := make(map[string]struct{})
	spans := []time.Time{}
	for _, entry := range timelineEntryEntities {
		value, ok := timelineCount[entry.Time]
		if !ok {
			spans = append(spans, entry.Time)
			value = make(types.VersionCountMap)
			timelineCount[entry.Time] = value
		}
		if entry.Version == "" {
			continue
		}
		allVersions[entry.Version] = struct{}{}
		versionCount, ok := value[entry.Version]
		if !ok {
			versionCount = entry.Total
		}
		value[entry.Version] = versionCount
	}

	for version := range allVersions {
		for timestamp := range timelineCount {
			if _, ok := timelineCount[timestamp][version]; !ok {
				timelineCount[timestamp][version] = 0
			}
		}
	}

	latestHistoryTime := time.Now()
	prevInstanceID := ""
	for _, instanceStatusHistory := range instanceWithStatusInInterval {
		for _, span := range spans {
			if prevInstanceID == "" {
				prevInstanceID = instanceStatusHistory.InstanceID
			}
			if prevInstanceID != instanceStatusHistory.InstanceID {
				prevInstanceID = instanceStatusHistory.InstanceID
				latestHistoryTime = time.Now()
			}
			if instanceStatusHistory.CreatedTs.After(span) {
				updateVersionTimeline(timelineCount, spans, instanceStatusHistory.CreatedTs, latestHistoryTime, instanceStatusHistory.Version)
				latestHistoryTime = instanceStatusHistory.CreatedTs
			}
		}
	}

	for _, vc := range versionCounts {
		if isNightlyVersion(vc.Version) {
			continue
		}
		for _, span := range spans {
			if _, ok := timelineCount[span][vc.Version]; !ok {
				timelineCount[span][vc.Version] = 0
			}
			timelineCount[span][vc.Version] += uint64(vc.Count)
		}
	}

	go func() {
		cachedGroupVersionCountLock.Lock()
		defer cachedGroupVersionCountLock.Unlock()
		val, ok := cachedGroupVersionCount[cacheKey]
		if !ok || time.Since(val.storedAt) >= cachedGroupVersionCountLifespan {
			l.Debug().Str("cacheStatus", "SET").Str("groupID", groupID).Str("duration", duration).Msg("GetGroupVersionCountTimeline")
			cachedGroupVersionCount[cacheKey] = groupVersionCountCache{timelineCount, time.Now()}
		}
	}()

	return timelineCount, false, nil
}

// GetGroupStatusCountTimeline computes the status x version count timeline
// for the given group/duration.
func (q *Queries) GetGroupStatusCountTimeline(groupID string, duration string) (map[time.Time](map[int](types.VersionCountMap)), error) {
	var timelineEntry []types.StatusVersionCountTimelineEntry
	durationString, interval, err := durationParamToPostgresTimings(durationParam(duration))
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
	WITH time_series AS (SELECT * FROM generate_series(now() - interval '%[1]s', now(), INTERVAL '%[2]s') AS ts),
	min_time AS (SELECT min(ts) AS min_ts FROM time_series),
	filtered_status_history AS (SELECT instance_status_history.* FROM instance_status_history,
		 min_time WHERE group_id=$1 AND %[3]s AND created_ts >= min_time.min_ts - INTERVAL '1 hour')
	SELECT ts, (CASE WHEN status IS NULL THEN 0 ELSE status END), 
	  (CASE WHEN version IS NULL THEN '' ELSE version END), count(instance_id) as total 
	FROM 
	(
		  SELECT * FROM time_series
		  LEFT JOIN(SELECT * FROM filtered_status_history) 
		  _ ON created_ts >= time_series.ts - INTERVAL '%[2]s' AND created_ts < time_series.ts
	) AS _
	GROUP BY 1,2,3
	ORDER BY ts DESC;
	`, durationString, interval, shared.IgnoreFakeInstanceCondition("instance_id"))
	rows, err := q.db.Queryx(query, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var timelineEntryEntity types.StatusVersionCountTimelineEntry
		if err := rows.StructScan(&timelineEntryEntity); err != nil {
			return nil, err
		}
		timelineEntry = append(timelineEntry, timelineEntryEntity)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	allStatuses := make(map[int]struct{})
	timelineCount := make(map[time.Time](map[int](types.VersionCountMap)))
	for _, entry := range timelineEntry {
		value, ok := timelineCount[entry.Time]
		if !ok {
			value = make(map[int](types.VersionCountMap))
			timelineCount[entry.Time] = value
		}
		if entry.Status == 0 {
			continue
		}
		allStatuses[entry.Status] = struct{}{}
		versionCount, ok := value[entry.Status]
		if !ok {
			versionCount = make(types.VersionCountMap)
		}
		if entry.Version == "" {
			continue
		}
		versionCount[entry.Version] = entry.Total
		value[entry.Status] = versionCount
	}

	for status := range allStatuses {
		for timestamp := range timelineCount {
			if _, ok := timelineCount[timestamp][status]; !ok {
				timelineCount[timestamp][status] = make(types.VersionCountMap)
			}
		}
	}

	return timelineCount, nil
}
