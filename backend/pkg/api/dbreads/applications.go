package dbreads

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/doug-martin/goqu/v9"
	"github.com/jmoiron/sqlx"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

// App ID cache
type appsCache map[string]string

var (
	cachedAppIDs      appsCache
	cachedAppsIDsLock sync.RWMutex
)

// InvalidateCachedAppIDs invalidates the cached app ID mappings.
func InvalidateCachedAppIDs() {
	cachedAppsIDsLock.Lock()
	cachedAppIDs = nil
	cachedAppsIDsLock.Unlock()
}

func (q *Queries) GetApp(appID string) (*api.Application, error) {
	var app api.Application
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
	app.Instances.Count, err = q.getInstanceCount(app.ID, "", shared.ValidityInterval)
	if err != nil {
		return nil, err
	}
	return &app, nil
}

func (q *Queries) GetAppsCount(teamID string) (int, error) {
	query := goqu.From("application").Where(goqu.C("team_id").Eq(teamID)).Select(goqu.L("count(*)"))
	return q.GetCountQuery(query)
}

func (q *Queries) GetApps(teamID string, page, perPage uint64) ([]*api.Application, error) {
	page, perPage = shared.ValidatePaginationParams(page, perPage)
	var apps []*api.Application
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
		app := api.Application{}
		err := rows.StructScan(&app)
		if err != nil {
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
		app.Instances.Count, err = q.getInstanceCount(app.ID, "", shared.ValidityInterval)
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
					app := api.Application{}
					err := rows.StructScan(&app)
					if err != nil {
						l.Warn().Err(err).Msg("Failed to read app from DB")
					}
					if prodIDPtr := app.ProductID.Ptr(); prodIDPtr != nil {
						prodIDLower := strings.ToLower(*prodIDPtr)
						cachedAppIDs[prodIDLower] = app.ID
					}
					cachedAppIDs[app.ID] = app.ID
				}
			} else {
				l.Error().Err(err).Msg("Failed to get apps")
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
		// Cache miss — the app may have arrived via replication after the cache
		// was built. Invalidate and rebuild once before giving up.
		InvalidateCachedAppIDs()
		return q.getAppIDFromCache(appIDNoBrackets, appOrProductID)
	}
	return cachedAppID, nil
}

// getAppIDFromCache does a single cache lookup after rebuild. Called only on
// cache miss to avoid infinite recursion.
func (q *Queries) getAppIDFromCache(normalizedID, originalID string) (string, error) {
	// Force cache rebuild by calling GetAppID logic with nil cache.
	// The cache was just invalidated by the caller.
	var cachedAppsRef appsCache
	cachedAppsIDsLock.Lock()
	if cachedAppIDs == nil {
		cachedAppIDs = make(appsCache)
		query, _, err := goqu.From("application").ToSQL()
		var rows *sqlx.Rows
		if err == nil {
			rows, err = q.db.Queryx(query)
		}
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				app := api.Application{}
				if err := rows.StructScan(&app); err != nil {
					l.Warn().Err(err).Msg("Failed to read app from DB")
				}
				if prodIDPtr := app.ProductID.Ptr(); prodIDPtr != nil {
					cachedAppIDs[strings.ToLower(*prodIDPtr)] = app.ID
				}
				cachedAppIDs[app.ID] = app.ID
			}
		}
	}
	cachedAppsRef = cachedAppIDs
	cachedAppsIDsLock.Unlock()

	cachedAppID, ok := cachedAppsRef[normalizedID]
	if !ok {
		return "", fmt.Errorf("no app found for ID %v", originalID)
	}
	return cachedAppID, nil
}

func (q *Queries) appsQuery() *goqu.SelectDataset {
	return goqu.From("application").
		Select("id", "product_id", "name", "description", "created_ts").
		Order(goqu.I("created_ts").Desc())
}

func (q *Queries) getInstanceCount(appID, groupID string, duration shared.PostgresDuration) (int, error) {
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
