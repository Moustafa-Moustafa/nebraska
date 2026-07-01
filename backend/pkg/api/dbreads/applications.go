package dbreads

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/doug-martin/goqu/v9"
	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog/log"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/types"
)

// appsCache maps application product ids (lower-cased) and UUIDs to the
// canonical application UUID. UUID -> UUID lets us also validate UUID inputs
// against existing apps.
type appsCache map[string]string

var (
	cachedAppIDs      appsCache
	cachedAppsIDsLock sync.RWMutex
)

// ClearCachedAppIDs invalidates the cached app IDs in cachedApps and
// must be called whenever the apps entries are modified.
func ClearCachedAppIDs() {
	cachedAppsIDsLock.Lock()
	cachedAppIDs = nil
	// Generating the map is not always possible here because the database
	// can be closed.
	cachedAppsIDsLock.Unlock()
}

// appsQuery returns a SelectDataset prepared to return all applications.
// Extended by callers to filter by id, team, etc.
func (q *Queries) appsQuery() *goqu.SelectDataset {
	return goqu.From("application").
		Select("id", "product_id", "name", "description", "created_ts").
		Order(goqu.I("created_ts").Desc())
}

// GetApp returns an application, hydrated with its groups, channels, and
// active instance count.
func (q *Queries) GetApp(appID string) (*types.Application, error) {
	var app types.Application
	query, _, err := goqu.From("application").
		Where(goqu.C("id").Eq(appID)).ToSQL()
	if err != nil {
		return nil, err
	}
	if err := q.db.QueryRowx(query).StructScan(&app); err != nil {
		return nil, err
	}
	groups, err := q.GetGroupsForApp(app.ID)
	if err == nil || err == sql.ErrNoRows {
		app.Groups = groups
	} else {
		return nil, err
	}
	channels, err := q.GetChannelsForApp(app.ID)
	if err == nil || err == sql.ErrNoRows {
		app.Channels = channels
	} else {
		return nil, err
	}
	app.Instances.Count, err = q.GetInstanceCount(app.ID, "", shared.ValidityInterval)
	if err != nil {
		return nil, err
	}
	return &app, nil
}

// GetAppsCount returns the number of applications owned by the given team.
func (q *Queries) GetAppsCount(teamID string) (int, error) {
	query := goqu.From("application").Where(goqu.C("team_id").Eq(teamID)).Select(goqu.L("count(*)"))
	return q.GetCountQuery(query)
}

// GetApps returns all applications that belong to the team id provided,
// paginated.
func (q *Queries) GetApps(teamID string, page, perPage uint64) ([]*types.Application, error) {
	page, perPage = shared.ValidatePaginationParams(page, perPage)
	var apps []*types.Application
	limit, offset := shared.SQLPaginate(page, perPage)
	query, _, err := q.appsQuery().
		Where(goqu.C("team_id").Eq(teamID)).
		Limit(limit).
		Offset(offset).
		ToSQL()
	if err != nil {
		return nil, err
	}
	rows, err := q.db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		app := types.Application{}
		if err := rows.StructScan(&app); err != nil {
			return nil, err
		}
		groups, err := q.GetGroupsForApp(app.ID)
		if err == nil || err == sql.ErrNoRows {
			app.Groups = groups
		} else {
			return nil, err
		}
		channels, err := q.GetChannelsForApp(app.ID)
		if err == nil || err == sql.ErrNoRows {
			app.Channels = channels
		} else {
			return nil, err
		}
		app.Instances.Count, err = q.GetInstanceCount(app.ID, "", shared.ValidityInterval)
		if err != nil {
			return nil, err
		}
		apps = append(apps, &app)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return apps, nil
}

// GetAppID resolves a product id (or already-canonical UUID) to the canonical
// application UUID. Uses a process-wide cache rebuilt on demand and
// invalidated by ClearCachedAppIDs.
func (q *Queries) GetAppID(appOrProductID string) (string, error) {
	var cachedAppsRef appsCache
	cachedAppsIDsLock.RLock()
	if cachedAppIDs != nil {
		cachedAppsRef = cachedAppIDs
	}
	cachedAppsIDsLock.RUnlock()

	if cachedAppsRef == nil {
		cachedAppsIDsLock.Lock()
		cachedAppsRef = cachedAppIDs
		if cachedAppsRef == nil {
			cachedAppIDs = make(appsCache)
			query, _, err := goqu.From("application").ToSQL()
			var rows *sqlx.Rows
			if err == nil {
				rows, err = q.db.Queryx(query)
			}
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					app := types.Application{}
					if err := rows.StructScan(&app); err != nil {
						log.Warn().Err(err).Msg("Failed to read app from DB")
					}
					if prodIDPtr := app.ProductID.Ptr(); prodIDPtr != nil {
						prodIDLower := strings.ToLower(*prodIDPtr)
						cachedAppIDs[prodIDLower] = app.ID
					}
					cachedAppIDs[app.ID] = app.ID
				}
			} else {
				log.Error().Err(err).Msg("Failed to get apps")
			}
			cachedAppsRef = cachedAppIDs
		}
		cachedAppsIDsLock.Unlock()
	}

	appIDNoBrackets := strings.TrimSpace(appOrProductID)
	lastIdx := len(appIDNoBrackets) - 1
	if len(appIDNoBrackets) > 2 && appIDNoBrackets[0] == '{' && appIDNoBrackets[lastIdx] == '}' {
		appIDNoBrackets = strings.TrimSpace(appIDNoBrackets[1:lastIdx])
	}
	appIDNoBrackets = strings.ToLower(appIDNoBrackets)

	cachedAppID, ok := cachedAppsRef[appIDNoBrackets]
	if !ok {
		return "", fmt.Errorf("no app found for ID %v", appOrProductID)
	}
	return cachedAppID, nil
}

// GetInstanceCount returns the number of distinct instance ids running the
// given application (or group, if groupID is non-empty) and still considered
// active per the provided duration window.
func (q *Queries) GetInstanceCount(appID, groupID string, duration shared.PostgresDuration) (int, error) {
	query, _, err := q.appInstancesCountQuery(appID, groupID, duration).ToSQL()
	if err != nil {
		return 0, err
	}
	count := 0
	if err := q.db.QueryRow(query).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (q *Queries) appInstancesCountQuery(appID, groupID string, duration shared.PostgresDuration) *goqu.SelectDataset {
	query := goqu.From("instance_application").
		Select(goqu.COUNT("*")).
		Where(
			goqu.L("last_check_for_updates > now() at time zone 'utc' - interval ?", duration),
			goqu.L(shared.IgnoreFakeInstanceCondition("instance_id")),
		)
	if appID != "" {
		query = query.Where(goqu.C("application_id").Eq(appID))
	}
	if groupID != "" {
		query = query.Where(goqu.C("group_id").Eq(groupID))
	}
	return query
}

// (no helper needed; callers use sql.ErrNoRows directly)
