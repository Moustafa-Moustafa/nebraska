package api

import (
	"github.com/doug-martin/goqu/v9"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/types"
)

const (
	activityPackageNotFound int = 1 + iota
	activityRolloutStarted
	activityRolloutFinished
	activityRolloutFailed
	activityInstanceUpdateFailed
	activityChannelPackageUpdated
)

const (
	activitySuccess int = 1 + iota
	activityInfo
	activityWarning
	activityError
)

type (
	Activity            = types.Activity
	ActivityQueryParams = types.ActivityQueryParams
)

// Gets the activity count using some ActivityQueryParams filters
// Page and PerPage are ignored.
// Start is nil, then it defaults -3 days.
// End is nil, then it defaults to Now.
func (api *API) GetActivityCount(teamID string, p ActivityQueryParams) (int, error) {
	return api.queries.GetActivityCount(teamID, p)
}

// GetActivity returns a list of activity entries that match the specified
// criteria in the query parameters.
func (api *API) GetActivity(teamID string, p ActivityQueryParams) ([]*Activity, error) {
	return api.queries.GetActivity(teamID, p)
}

// hasRecentRuntimeActivity reports whether there is matching runtime activity
// entry in the last 24h. Only runtime classes (1-5) are meaningful here.
// Admin events live in admin_activity and are not returned here.
func (api *API) hasRecentRuntimeActivity(class int, p ActivityQueryParams) bool {
	return api.queries.HasRecentRuntimeActivity(class, p)
}

// newGroupActivityEntry creates a new activity entry related to a specific
// group.
func (api *API) newGroupActivityEntry(class int, severity int, version, appID, groupID string) error {
	query, _, err := goqu.Insert("activity").
		Cols("class", "severity", "version", "application_id", "group_id").
		Vals(goqu.Vals{class, severity, version, appID, groupID}).
		ToSQL()
	if err != nil {
		return err
	}
	_, err = api.db.Exec(query)

	if err != nil {
		return err
	}

	return nil
}

// newChannelActivityEntry creates a new admin_activity entry related to a
// specific channel.
func (api *API) newChannelActivityEntry(class int, severity int, version, appID, channelID string) error {
	query, _, err := goqu.Insert("admin_activity").
		Cols("class", "severity", "version", "application_id", "channel_id").
		Vals(goqu.Vals{class, severity, version, appID, channelID}).
		ToSQL()
	if err != nil {
		return err
	}
	_, err = api.db.Exec(query)

	if err != nil {
		return err
	}

	return nil
}

// newInstanceActivityEntry creates a new activity entry related to a specific
// instance.
func (api *API) newInstanceActivityEntry(class int, severity int, version, appID, groupID, instanceID string) error {
	query, _, err := goqu.Insert("activity").
		Cols("class", "severity", "version", "application_id", "group_id", "instance_id").
		Vals(goqu.Vals{class, severity, version, appID, groupID, instanceID}).
		ToSQL()
	if err != nil {
		return err
	}
	_, err = api.db.Exec(query)

	if err != nil {
		return err
	}

	return nil
}
