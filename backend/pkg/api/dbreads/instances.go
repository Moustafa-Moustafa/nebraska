package dbreads

import (
	"database/sql"
	"strconv"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/doug-martin/goqu/v9/exp"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

type sortOrder int
const (
	sortOrderAsc sortOrder = iota
	sortOrderDesc
)

const defaultInterval = 2 * time.Hour

func (q *Queries) GetInstance(instanceID, appID string) (*api.Instance, error) {
	var instance api.Instance
	query, _, err := goqu.From("instance").
		Where(goqu.C("id").Eq(instanceID)).
		ToSQL()
	if err != nil {
		return nil, err
	}
	err = q.db.QueryRowx(query).StructScan(&instance)
	if err != nil {
		return nil, err
	}
	instanceApplication, err := q.getInstanceApp(appID, instance.ID, shared.ValidityInterval, "", 0)
	switch err {
	case nil:
		instance.Application = *instanceApplication
	case sql.ErrNoRows:
		instance.Application = api.InstanceApplication{}
	default:
		return nil, err
	}
	return &instance, nil
}

func (q *Queries) getInstanceApp(appID, instanceID string, duration shared.PostgresDuration, sortFilter string, orderOfSort sortOrder) (*api.InstanceApplication, error) {
	var instanceApp api.InstanceApplication
	query, _, err := q.instanceAppQuery(appID, instanceID, duration, sortFilter, orderOfSort).ToSQL()
	if err != nil {
		return nil, err
	}
	err = q.db.QueryRowx(query).StructScan(&instanceApp)
	if err != nil {
		return nil, err
	}
	return &instanceApp, nil
}

func (q *Queries) GetInstanceStatusHistory(instanceID, appID, groupID string, limit uint64) ([]*api.InstanceStatusHistoryEntry, error) {
	var history []*api.InstanceStatusHistoryEntry
	query, _, err := q.instanceStatusHistoryQuery(instanceID, appID, groupID, limit).ToSQL()
	if err != nil {
		return nil, err
	}
	rows, err := q.db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry api.InstanceStatusHistoryEntry
		err = rows.StructScan(&entry)
		if err != nil {
			return nil, err
		}
		if entry.Status == api.InstanceStatusError {
			entry.ErrorCode, err = q.GetEvent(instanceID, appID, entry.CreatedTs)
			if err != nil {
				return nil, err
			}
		}
		history = append(history, &entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return history, nil
}

func (q *Queries) GetInstances(p api.InstancesQueryParams, duration string) (api.InstancesWithTotal, error) {
	var instances []*api.Instance
	totalCount, err := q.GetInstancesCount(p, duration)
	if err != nil {
		return api.InstancesWithTotal{}, err
	}
	p.Page, p.PerPage = shared.ValidatePaginationParams(p.Page, p.PerPage)
	var dbDuration shared.PostgresDuration
	dbDuration, _, err = durationParamToPostgresTimings(durationParam(duration))
	if err != nil {
		return api.InstancesWithTotal{}, err
	}

	limit, offset := shared.SQLPaginate(p.Page, p.PerPage)
	sortFilter := sanitizeSortFilterParams(p.SortFilter)
	sortOrder := sortOrderFromString(p.SortOrder)
	instancesQuery := q.instancesQuery(p, dbDuration)
	instancesQuery = instancesQuery.Select("id", "ip", "created_ts", goqu.Case().
		When(goqu.C("alias").Neq(""), goqu.C("alias")).Else(goqu.C("id")).As("alias"))

	instanceAppQuery := prepareInstanceAppQuery()
	finalQuery := prepareGetInstancesQuery(instancesQuery, instanceAppQuery)
	switch sortOrder {
	case sortOrderAsc:
		finalQuery = finalQuery.Order(goqu.I(sortFilter).Asc().NullsLast())
	case sortOrderDesc:
		finalQuery = finalQuery.Order(goqu.I(sortFilter).Desc().NullsLast())
	}

	finalQuery = prepareSearchQuery(finalQuery, p)
	query, _, err := finalQuery.
		Limit(limit).
		Offset(offset).
		ToSQL()
	if err != nil {
		return api.InstancesWithTotal{}, err
	}
	rows, err := q.db.Queryx(query)
	if err != nil {
		return api.InstancesWithTotal{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var instance api.Instance
		err = rows.Scan(&instance.ID, &instance.IP, &instance.CreatedTs, &instance.Alias,
			&instance.Application.Version, &instance.Application.Status, &instance.Application.LastCheckForUpdates,
			&instance.Application.LastUpdateVersion, &instance.Application.UpdateInProgress,
			&instance.Application.ApplicationID, &instance.Application.GroupID, &instance.Application.InstanceID)
		if err != nil {
			return api.InstancesWithTotal{}, err
		}
		instances = append(instances, &instance)
	}
	if err := rows.Err(); err != nil {
		return api.InstancesWithTotal{}, err
	}
	return api.InstancesWithTotal{
		TotalInstances: uint64(totalCount),
		Instances:      instances,
	}, nil
}

func (q *Queries) GetInstancesCount(p api.InstancesQueryParams, duration string) (int, error) {
	var dbDuration shared.PostgresDuration
	dbDuration, _, err := durationParamToPostgresTimings(durationParam(duration))
	if err != nil {
		return 0, err
	}
	instancesQuery := q.instancesQuery(p, dbDuration)
	instancesQuery = instancesQuery.Select("id", "ip", "created_ts", goqu.Case().
		When(goqu.C("alias").Neq(""), goqu.C("alias")).Else(goqu.C("id")).As("alias"))

	instanceAppQuery := prepareInstanceAppQuery()
	finalQuery := prepareGetInstancesQuery(instancesQuery, instanceAppQuery)
	finalQuery = prepareSearchQuery(finalQuery, p).Select(goqu.L("COUNT(*)"))

	return q.GetCountQuery(finalQuery)
}

func (q *Queries) instanceAppQuery(appID, instanceID string, duration shared.PostgresDuration, sortFilter string, orderOfSort sortOrder) *goqu.SelectDataset {
	query := prepareInstanceAppQuery().Where(goqu.C("application_id").Eq(appID)).
		Where(goqu.L("last_check_for_updates > now() at time zone 'utc' - interval ?", duration))
	if instanceID != "" {
		query = query.Where(goqu.C("instance_id").Eq(instanceID))
	}
	if sortFilter != "" {
		switch orderOfSort {
		case sortOrderAsc:
			query = query.Order(goqu.I(sortFilter).Asc().NullsLast())
		case sortOrderDesc:
			query = query.Order(goqu.I(sortFilter).Desc().NullsLast())
		}
	}
	return query
}

func (q *Queries) getFilterInstancesQuery(selectPart exp.LiteralExpression, p api.InstancesQueryParams, duration shared.PostgresDuration) *goqu.SelectDataset {
	query := goqu.From("instance_application").
		Select(selectPart).
		Where(goqu.C("application_id").Eq(p.ApplicationID), goqu.C("group_id").Eq(p.GroupID)).
		Where(goqu.L("last_check_for_updates > now() at time zone 'utc' - interval ?", duration),
			goqu.L(shared.IgnoreFakeInstanceCondition("instance_id")))
	if p.Status == api.InstanceStatusUndefined {
		query = query.Where(goqu.L("status IS NULL"))
	} else if p.Status != 0 {
		query = query.Where(goqu.C("status").Eq(p.Status))
	}
	if p.Version != "" {
		query = query.Where(goqu.C("version").Eq(p.Version))
	}
	return query
}

func (q *Queries) instancesQuery(p api.InstancesQueryParams, duration shared.PostgresDuration) *goqu.SelectDataset {
	instancesSubquery := q.getFilterInstancesQuery(goqu.L("instance_id"), p, duration)
	return goqu.From("instance").
		Where(goqu.L("id IN ?", instancesSubquery))
}

func (q *Queries) instanceStatusHistoryQuery(instanceID, appID, groupID string, limit uint64) *goqu.SelectDataset {
	if limit == 0 {
		limit = 20
	}
	return goqu.From("instance_status_history").Where(goqu.C("instance_id").Eq(instanceID)).
		Where(goqu.C("application_id").Eq(appID)).
		Where(goqu.C("group_id").Eq(groupID)).
		Order(goqu.C("created_ts").Desc()).
		Limit(uint(limit))
}

func (q *Queries) GetDefaultInterval() time.Duration {
	return defaultInterval
}

func (q *Queries) GetInstanceStats() ([]api.InstanceStats, error) {
	query, _, err := goqu.From("instance_stats").
		Order(goqu.C("timestamp").Asc()).ToSQL()
	if err != nil {
		return nil, err
	}
	rows, err := q.db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var instances []api.InstanceStats
	for rows.Next() {
		var instance api.InstanceStats
		err = rows.StructScan(&instance)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

func (q *Queries) GetInstanceStatsByTimestamp(t time.Time) ([]api.InstanceStats, error) {
	timestamp := goqu.L("timestamp ?", goqu.V(t.Format("2006-01-02T15:04:05.999999Z07:00")))
	query, _, err := goqu.From("instance_stats").
		Where(goqu.C("timestamp").Eq(timestamp)).
		Order(goqu.C("version").Asc()).ToSQL()
	if err != nil {
		return nil, err
	}
	rows, err := q.db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var instances []api.InstanceStats
	for rows.Next() {
		var instance api.InstanceStats
		err = rows.StructScan(&instance)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	return instances, nil
}
type instanceFilterItem int

const (
	instanceFilterID instanceFilterItem = iota
	instanceFilterIP
	instanceFilterLastCheck
)

var sortFilterMap = map[instanceFilterItem]string{
	instanceFilterID:        "alias",
	instanceFilterIP:        "ip",
	instanceFilterLastCheck: "last_check_for_updates",
}

func sortOrderFromString(str string) sortOrder {
	val, err := strconv.Atoi(str)
	if (val != 0 && val != 1) || err != nil {
		return sortOrderDesc
	}
	return sortOrder(val)
}

func sanitizeSortFilterParams(sortFilter string) string {
	sortFilterNumericValue, _ := strconv.Atoi(sortFilter)
	if value, ok := sortFilterMap[instanceFilterItem(sortFilterNumericValue)]; ok {
		return value
	}
	return sortFilterMap[instanceFilterID]
}

func prepareInstanceAppQuery() *goqu.SelectDataset {
	return goqu.From("instance_application").
		Select("version", "status", "last_check_for_updates", "last_update_version", "update_in_progress", "application_id", "group_id", "instance_id")
}

func prepareGetInstancesQuery(instanceQuery *goqu.SelectDataset, instanceAppQuery *goqu.SelectDataset) *goqu.SelectDataset {
	return goqu.From(goqu.L("Instance")).With("Instance", instanceQuery).With("application", instanceAppQuery).InnerJoin(
		goqu.L("application"),
		goqu.On(goqu.L("Instance.id").Eq(goqu.L("application.instance_id"))),
	).Select(goqu.L("*"))
}

func prepareSearchQuery(finalQuery *goqu.SelectDataset, p api.InstancesQueryParams) *goqu.SelectDataset {
	searchFilter := p.SearchFilter
	searchValue := p.SearchValue
	searchExpression := "%" + searchValue + "%"
	outputQuery := finalQuery
	if searchFilter == "All" && searchValue != "" {
		outputQuery = finalQuery.Where(
			goqu.Or(goqu.I("alias").ILike(searchExpression),
				goqu.I("id").ILike(searchExpression),
				goqu.L("text(ip)").Like(searchExpression)))
	} else if searchFilter != "" && searchValue != "" {
		if searchFilter == "ip" {
			outputQuery = finalQuery.Where(
				goqu.L("text(ip)").Like(searchExpression))
		} else {
			outputQuery = finalQuery.Where(
				goqu.I(searchFilter).ILike(searchExpression))
		}
	}
	return outputQuery
}
