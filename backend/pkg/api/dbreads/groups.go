package dbreads

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/doug-martin/goqu/v9"
	"golang.org/x/sync/errgroup"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

// Cache for group track names
var (
	cachedGroups     map[api.GroupDescriptor]string
	cachedGroupsLock sync.RWMutex

	cachedGroupVersionCount         = make(map[groupDurationCacheKey]groupVersionCountCache)
	cachedGroupVersionCountLock     sync.RWMutex
	cachedGroupVersionCountLifespan = time.Minute
)

// SetCacheLifespanForTest sets the cache lifespan and returns the old value.
// For testing only.
func SetCacheLifespanForTest(lifespan time.Duration) time.Duration {
	old := cachedGroupVersionCountLifespan
	cachedGroupVersionCountLifespan = lifespan
	return old
}

type groupDurationCacheKey struct {
	GroupID  string
	Duration string
}

type groupVersionCountCache struct {
	data     map[time.Time](api.VersionCountMap)
	storedAt time.Time
}

// Duration helpers
type durationParam string
type durationCode int
type postgresInterval string

const deadInstanceTimeSpan = "6 months"

const (
	oneHour durationCode = iota
	oneDay
	sevenDays
	thirtyDays
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

func isNightlyVersion(version string) bool {
	return strings.Contains(version, "nightly")
}

func updateVersionTimeline(timeline map[time.Time]api.VersionCountMap, spans []time.Time, from time.Time, to time.Time, version string) {
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

// InvalidateCachedGroups invalidates the cached track names.
func InvalidateCachedGroups() {
	cachedGroupsLock.Lock()
	cachedGroups = nil
	cachedGroupsLock.Unlock()
}

func (q *Queries) GetGroup(groupID string) (*api.Group, error) {
	var group api.Group
	query := `SELECT g.*, gs.rollout_in_progress, gs.safe_mode_disabled
		FROM groups g LEFT JOIN group_state gs ON g.id = gs.group_id
		WHERE g.id = $1`
	err := q.db.QueryRowx(query, groupID).StructScan(&group)
	if err != nil {
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

func (q *Queries) GetGroupID(appID, trackName string, arch api.Arch) (string, error) {
	var cachedGroupsRef map[api.GroupDescriptor]string
	cachedGroupsLock.RLock()
	if cachedGroups != nil {
		cachedGroupsRef = cachedGroups
	}
	cachedGroupsLock.RUnlock()
	if cachedGroupsRef == nil {
		cachedGroupsLock.Lock()
		cachedGroupsRef = cachedGroups
		if cachedGroupsRef == nil {
			cachedGroups = make(map[api.GroupDescriptor]string)
			query, _, err := goqu.From("groups").ToSQL()
			var groups []*api.Group
			if err == nil {
				groups, err = q.getGroupsFromQuery(query)
			}
			if err != nil {
				l.Error().Err(err).Msg("GetGroupID error")
			} else {
				for _, group := range groups {
					if group.Channel != nil {
						descriptor := api.GroupDescriptor{AppID: group.ApplicationID, Track: group.Track, Arch: group.Channel.Arch}
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

	cachedGroupID, ok := cachedGroupsRef[api.GroupDescriptor{AppID: appIDNoBrackets, Track: trackName, Arch: arch}]
	if !ok {
		// Cache miss — group may have arrived via replication. Invalidate and retry once.
		InvalidateCachedGroups()
		return q.getGroupIDRetry(appIDNoBrackets, trackName, arch, appID)
	}
	return cachedGroupID, nil
}

func (q *Queries) getGroupIDRetry(normalizedAppID, trackName string, arch api.Arch, originalAppID string) (string, error) {
	cachedGroupsLock.Lock()
	if cachedGroups == nil {
		cachedGroups = make(map[api.GroupDescriptor]string)
		query, _, err := goqu.From("groups").ToSQL()
		var groups []*api.Group
		if err == nil {
			groups, err = q.getGroupsFromQuery(query)
		}
		if err == nil {
			for _, group := range groups {
				if group.Channel != nil {
					cachedGroups[api.GroupDescriptor{AppID: group.ApplicationID, Track: group.Track, Arch: group.Channel.Arch}] = group.ID
				}
			}
		}
	}
	ref := cachedGroups
	cachedGroupsLock.Unlock()

	if id, ok := ref[api.GroupDescriptor{AppID: normalizedAppID, Track: trackName, Arch: arch}]; ok {
		return id, nil
	}
	return "", fmt.Errorf("no group found for app %v, track %v, and architecture %v", originalAppID, trackName, arch)
}

func (q *Queries) GetGroupsCount(appID string) (int, error) {
	query := goqu.From("groups").Where(goqu.C("application_id").Eq(appID)).Select(goqu.L("count(*)"))
	return q.GetCountQuery(query)
}

func (q *Queries) GetGroups(appID string, page, perPage uint64) ([]*api.Group, error) {
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

func (q *Queries) GetGroupsForApp(appID string) ([]*api.Group, error) {
	query, _, err := q.groupsQuery().Where(goqu.C("application_id").Eq(appID)).ToSQL()
	if err != nil {
		return nil, err
	}
	return q.getGroupsFromQuery(query)
}

func (q *Queries) getGroupsFromQuery(query string) ([]*api.Group, error) {
	var groups []*api.Group
	rows, err := q.db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		group := api.Group{}
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

func (q *Queries) GetGroupUpdatesStats(group *api.Group) (*api.UpdatesStats, error) {
	var updatesStats api.UpdatesStats
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
	).Where(goqu.C("group_id").Eq(group.ID), goqu.L("last_check_for_updates > now() at time zone 'utc' - interval ?", shared.ValidityInterval),
		goqu.L(shared.IgnoreFakeInstanceCondition("instance_id")),
	).ToSQL()
	if err != nil {
		return nil, err
	}
	err = q.db.QueryRowx(query).StructScan(&updatesStats)
	if err != nil {
		return nil, err
	}
	return &updatesStats, nil
}

func (q *Queries) groupsQuery() *goqu.SelectDataset {
	return goqu.From(goqu.T("groups").As("g")).
		Select(
			goqu.I("g.*"),
			goqu.I("gs.rollout_in_progress"),
			goqu.I("gs.safe_mode_disabled"),
		).
		LeftJoin(goqu.T("group_state").As("gs"), goqu.On(goqu.I("g.id").Eq(goqu.I("gs.group_id")))).
		Order(goqu.I("g.created_ts").Desc())
}

func (q *Queries) GetGroupVersionBreakdown(groupID string) ([]*api.VersionBreakdownEntry, error) {
	var entryList []*api.VersionBreakdownEntry
	query := fmt.Sprintf(`
	SELECT version, count(*) as instances, (count(*) * 100.0 / total) as percentage
	FROM instance_application, (
		SELECT count(*) as total
		FROM instance_application
		WHERE group_id=$1 AND last_check_for_updates > now() at time zone 'utc' - interval '%[1]s'
		) totals
	WHERE group_id=$1 AND last_check_for_updates > now() at time zone 'utc' - interval '%[1]s' AND %[2]s
	GROUP BY version, total
	ORDER BY regexp_matches(version, '(\d+)\.(\d+)\.(\d+)')::int[] DESC
	`, shared.ValidityInterval, shared.IgnoreFakeInstanceCondition("instance_id"))
	rows, err := q.db.Queryx(query, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry api.VersionBreakdownEntry
		err := rows.StructScan(&entry)
		if err != nil {
			return nil, err
		}
		entryList = append(entryList, &entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entryList, nil
}

func (q *Queries) GetGroupInstancesStats(groupID, duration string) (*api.InstancesStatusStats, error) {
	var instancesStats api.InstancesStatusStats
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
	completedExpr := goqu.L("case when status = ? then 1 else 0 end", api.InstanceStatusComplete)
	if packageVersion != "" {
		undefinedExpr = goqu.L("case when version != ? and status IS NULL then 1 else 0 end", packageVersion)
		completedExpr = goqu.L("case when (version = ? and status IS NULL) or (status = ?) then 1 else 0 end", packageVersion, api.InstanceStatusComplete)
	}

	query, _, err := goqu.From("instance_application").Select(
		goqu.COUNT("*").As("total"),
		goqu.COALESCE(goqu.SUM(undefinedExpr), 0).As("undefined"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when status = ? then 1 else 0 end", api.InstanceStatusError)), 0).As("error"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when status = ? then 1 else 0 end", api.InstanceStatusUpdateGranted)), 0).As("update_granted"),
		goqu.COALESCE(goqu.SUM(completedExpr), 0).As("complete"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when status = ? then 1 else 0 end", api.InstanceStatusInstalled)), 0).As("installed"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when status = ? then 1 else 0 end", api.InstanceStatusDownloaded)), 0).As("downloaded"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when status = ? then 1 else 0 end", api.InstanceStatusDownloading)), 0).As("downloading"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when status = ? then 1 else 0 end", api.InstanceStatusOnHold)), 0).As("onhold"),
	).Where(goqu.C("group_id").Eq(groupID), goqu.L("last_check_for_updates > now() at time zone 'utc' - interval ?", durationString),
		goqu.L(shared.IgnoreFakeInstanceCondition("instance_id")),
	).ToSQL()
	if err != nil {
		return nil, err
	}
	err = q.db.QueryRowx(query).StructScan(&instancesStats)
	if err != nil {
		return nil, err
	}
	return &instancesStats, nil
}

func (q *Queries) GetGroupVersionCountTimeline(groupID string, duration string) (map[time.Time](api.VersionCountMap), bool, error) {
	cacheKey := groupDurationCacheKey{GroupID: groupID, Duration: duration}

	cachedGroupVersionCountLock.RLock()
	val, ok := cachedGroupVersionCount[cacheKey]
	cachedGroupVersionCountLock.RUnlock()
	if ok {
		if time.Since(val.storedAt) < cachedGroupVersionCountLifespan {
			return val.data, true, nil
		}
	}

	durationString, interval, err := durationParamToPostgresTimings(durationParam(duration))
	if err != nil {
		return nil, false, err
	}

	queryWg := new(errgroup.Group)
	timelineCount := make(map[time.Time]api.VersionCountMap)
	timelineEntryEntities := []api.VersionCountTimelineEntry{}

	queryWg.Go(func() error {
		instancesWithoutStatusQuery := fmt.Sprintf(`with time_series as ( select * from generate_series( now() - interval '%[2]s', now(), interval '%[3]s' ) as ts ), instances as ( select ia.instance_id, case when last_update_granted_ts is not null then last_update_granted_ts else ia.created_ts end, ia."version" from instance_application ia left join ( select * from instance_status_history where group_id = '%[1]s' and created_ts >= now() - interval '%[4]s' and status = 4 ) ish on ia.instance_id = ish.instance_id where (ia."group_id" = '%[1]s') and last_check_for_updates >= now() - interval '%[2]s' and ( ia.instance_id is null or ia.instance_id not like '{________-____-____-____-____________}' ) and ia.version not like '%%nightly%%' and ish.instance_id is null ) select ts, ( case when version is null then '' else version end ), sum( case when version is not null then 1 else 0 end ) total from ( select * from time_series left join ( select * from instances ) _ on created_ts <= time_series.ts ) as _ group by 1, 2 order by ts desc;`, groupID, durationString, interval, deadInstanceTimeSpan)
		rows, err := q.db.Queryx(instancesWithoutStatusQuery)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e api.VersionCountTimelineEntry
			if err := rows.StructScan(&e); err != nil {
				return err
			}
			timelineEntryEntities = append(timelineEntryEntities, e)
		}
		return nil
	})

	instanceWithStatusInInterval := []api.InstanceStatusHistoryEntry{}
	queryWg.Go(func() error {
		q2 := fmt.Sprintf(`select * from instance_status_history ish inner join (select instance_id from instance_application where ( "group_id" = '%[1]s' ) AND last_check_for_updates >= now() - interval '%[2]s' AND ( instance_id IS NULL OR instance_id NOT LIKE '{________-____-____-____-____________}')) ia on ish.instance_id = ia.instance_id  where ish.group_id = '%[1]s' and ish.status=4 and ish.created_ts >= now()-interval '%[2]s' order by ish.instance_id,ish.created_ts desc;`, groupID, durationString)
		rows, err := q.db.Queryx(q2)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			rI := api.InstanceStatusHistoryEntry{}
			if err := rows.StructScan(&rI); err != nil {
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
		q3 := fmt.Sprintf(`with active_instance as (select instance_id from instance_application where group_id = '%[1]s' and last_check_for_updates >= now() - interval '%[2]s' and( instance_id IS NULL OR instance_id NOT LIKE '{________-____-____-____-____________}')) , instance_status_interval as (select distinct instance_id from instance_status_history where instance_id in (select instance_id from active_instance) and status = 4 and created_ts >= now()-interval '%[2]s'), instance_status_to_process as (select ai.instance_id from active_instance ai left join instance_status_interval isi on ai.instance_id = isi.instance_id where isi.instance_id is null) select version as version, count(*) as count from (select distinct on (instance_id) instance_id,id,status,version,created_ts,application_id,group_id from instance_status_history where instance_id in (select instance_id from instance_status_to_process) and status=4 and created_ts >= now()-interval '%[3]s' order by instance_id,created_ts desc)_ group by version;`, groupID, durationString, deadInstanceTimeSpan)
		rows, err := q.db.Queryx(q3)
		if err != nil {
			return err
		}
		for rows.Next() {
			vc := versionCount{}
			if err = rows.StructScan(&vc); err != nil {
				return err
			}
			versionCounts = append(versionCounts, vc)
		}
		return nil
	})

	if err = queryWg.Wait(); err != nil {
		return nil, false, err
	}

	allVersions := make(map[string]struct{})
	spans := []time.Time{}
	for _, entry := range timelineEntryEntities {
		value, ok := timelineCount[entry.Time]
		if !ok {
			spans = append(spans, entry.Time)
			value = make(api.VersionCountMap)
			timelineCount[entry.Time] = value
		}
		if entry.Version == "" {
			continue
		}
		allVersions[entry.Version] = struct{}{}
		vc, ok := value[entry.Version]
		if !ok {
			vc = entry.Total
		}
		value[entry.Version] = vc
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
	for _, ish := range instanceWithStatusInInterval {
		for _, span := range spans {
			if prevInstanceID == "" {
				prevInstanceID = ish.InstanceID
			}
			if prevInstanceID != ish.InstanceID {
				prevInstanceID = ish.InstanceID
				latestHistoryTime = time.Now()
			}
			if ish.CreatedTs.After(span) {
				updateVersionTimeline(timelineCount, spans, ish.CreatedTs, latestHistoryTime, ish.Version)
				latestHistoryTime = ish.CreatedTs
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
		v, ok := cachedGroupVersionCount[cacheKey]
		if !ok || time.Since(v.storedAt) >= cachedGroupVersionCountLifespan {
			cachedGroupVersionCount[cacheKey] = groupVersionCountCache{timelineCount, time.Now()}
		}
	}()

	return timelineCount, false, nil
}

func (q *Queries) GetGroupStatusCountTimeline(groupID string, duration string) (map[time.Time](map[int](api.VersionCountMap)), error) {
	var timelineEntry []api.StatusVersionCountTimelineEntry
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
	FROM (
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
		var e api.StatusVersionCountTimelineEntry
		if err := rows.StructScan(&e); err != nil {
			return nil, err
		}
		timelineEntry = append(timelineEntry, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	allStatuses := make(map[int]struct{})
	timelineCount := make(map[time.Time](map[int](api.VersionCountMap)))

	for _, entry := range timelineEntry {
		value, ok := timelineCount[entry.Time]
		if !ok {
			value = make(map[int](api.VersionCountMap))
			timelineCount[entry.Time] = value
		}
		if entry.Status == 0 {
			continue
		}
		allStatuses[entry.Status] = struct{}{}
		vc, ok := value[entry.Status]
		if !ok {
			vc = make(api.VersionCountMap)
		}
		if entry.Version == "" {
			continue
		}
		vc[entry.Version] = entry.Total
		value[entry.Status] = vc
	}

	for status := range allStatuses {
		for timestamp := range timelineCount {
			if _, ok := timelineCount[timestamp][status]; !ok {
				timelineCount[timestamp][status] = make(api.VersionCountMap)
			}
		}
	}

	return timelineCount, nil
}
