package api

import (
	"errors"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/google/uuid"

	"github.com/flatcar/nebraska/backend/pkg/api/dbreads"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/types"
)

// Group, GroupDescriptor, VersionBreakdownEntry, VersionCountTimelineEntry,
// StatusVersionCountTimelineEntry, VersionCountMap, InstancesStatusStats, and
// UpdatesStats are owned by pkg/api/internal/types; re-exported here.
type (
	Group                           = types.Group
	GroupDescriptor                 = types.GroupDescriptor
	VersionBreakdownEntry           = types.VersionBreakdownEntry
	VersionCountTimelineEntry       = types.VersionCountTimelineEntry
	StatusVersionCountTimelineEntry = types.StatusVersionCountTimelineEntry
	VersionCountMap                 = types.VersionCountMap
	InstancesStatusStats            = types.InstancesStatusStats
	UpdatesStats                    = types.UpdatesStats
)

// postgresDuration is the Postgres INTERVAL type used by writer-side helpers
// in applications.go / instances.go that still take a typed duration value.
// Kept here as an alias so those callers compile unchanged.
type postgresDuration = shared.PostgresDuration

var (
	// ErrInvalidChannel error indicates that a channel doesn't belong to the
	// application it was supposed to belong to.
	ErrInvalidChannel = errors.New("nebraska: invalid channel")

	// ErrExpectingValidTimezone error indicates that a valid timezone wasn't
	// provided when enabling the flag PolicyOfficeHours.
	ErrExpectingValidTimezone = errors.New("nebraska: expecting valid timezone")
)

// AddGroup registers the provided group.
func (api *API) AddGroup(group *Group) (*Group, error) {
	if group.PolicyOfficeHours && !isTimezoneValid(group.PolicyTimezone.String) {
		return nil, ErrExpectingValidTimezone
	}

	if group.ChannelID.String != "" {
		if err := api.validateChannel(group.ChannelID.String, group.ApplicationID); err != nil {
			return nil, err
		}
	}
	// Instead of trying to solve this in the database, generate the ID beforehand to copy it to the track.
	if group.ID == "" {
		group.ID = uuid.New().String()
	}
	if group.Track == "" {
		group.Track = group.ID
	}
	query, _, err := goqu.Insert("groups").
		Cols("id", "name", "description", "application_id", "channel_id", "policy_updates_enabled", "policy_safe_mode", "policy_office_hours",
			"policy_timezone", "policy_period_interval", "policy_max_updates_per_period", "policy_update_timeout", "track").
		Vals(goqu.Vals{
			group.ID,
			group.Name,
			group.Description,
			group.ApplicationID,
			group.ChannelID,
			group.PolicyUpdatesEnabled,
			group.PolicySafeMode,
			group.PolicyOfficeHours,
			group.PolicyTimezone,
			group.PolicyPeriodInterval,
			group.PolicyMaxUpdatesPerPeriod,
			group.PolicyUpdateTimeout,
			group.Track,
		}).
		Returning(goqu.T("groups").All()).
		ToSQL()
	if err != nil {
		return nil, err
	}
	err = api.db.QueryRowx(query).StructScan(group)
	if err != nil {
		return nil, err
	}
	api.updateCachedGroups()
	// Re-read through groupsQuery so the returned struct reflects the joined
	// group_local row.
	return api.GetGroup(group.ID)
}

// UpdateGroup updates an existing group using the context of the group
// provided.
func (api *API) UpdateGroup(group *Group) error {
	if group.PolicyOfficeHours && !isTimezoneValid(group.PolicyTimezone.String) {
		return ErrExpectingValidTimezone
	}

	groupBeforeUpdate, err := api.GetGroup(group.ID)
	if err != nil {
		return err
	}

	if group.ChannelID.String != "" {
		if err := api.validateChannel(group.ChannelID.String, groupBeforeUpdate.ApplicationID); err != nil {
			return err
		}
	}
	if group.Track == "" {
		group.Track = group.ID
	}
	query, _, err := goqu.Update("groups").
		Set(
			goqu.Record{
				"name":                          group.Name,
				"description":                   group.Description,
				"channel_id":                    group.ChannelID,
				"policy_updates_enabled":        group.PolicyUpdatesEnabled,
				"policy_safe_mode":              group.PolicySafeMode,
				"policy_office_hours":           group.PolicyOfficeHours,
				"policy_timezone":               group.PolicyTimezone,
				"policy_period_interval":        group.PolicyPeriodInterval,
				"policy_max_updates_per_period": group.PolicyMaxUpdatesPerPeriod,
				"policy_update_timeout":         group.PolicyUpdateTimeout,
				"track":                         group.Track,
			},
		).
		Where(goqu.C("id").Eq(group.ID)).
		ToSQL()
	if err != nil {
		return err
	}
	result, err := api.db.Exec(query)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNoRowsAffected
	}
	api.updateCachedGroups()
	return nil
}

// ClearUpdatesEnabledOverride clears the local policy_updates_enabled override
// on the group_local row so the admin default on groups takes effect again on
// this node.
func (api *API) ClearUpdatesEnabledOverride(groupID string) error {
	query, _, err := goqu.Update("group_local").
		Set(goqu.Record{"policy_updates_enabled_override": nil}).
		Where(goqu.C("group_id").Eq(groupID)).
		ToSQL()
	if err != nil {
		return err
	}
	result, err := api.db.Exec(query)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNoRowsAffected
	}
	return nil
}

// DeleteGroup removes the group identified by the id provided.
func (api *API) DeleteGroup(groupID string) error {
	query, _, err := goqu.Delete("groups").Where(goqu.C("id").Eq(groupID)).ToSQL()
	if err != nil {
		return err
	}
	result, err := api.db.Exec(query)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrNoRowsAffected
	}
	api.updateCachedGroups()
	return nil
}

// GetGroup forwards to api.queries; SQL in pkg/api/dbreads/groups.go.
func (api *API) GetGroup(groupID string) (*Group, error) {
	return api.queries.GetGroup(groupID)
}

// GetGroupID forwards to api.queries; cache + SQL in pkg/api/dbreads/groups.go.
func (api *API) GetGroupID(appID, trackName string, arch Arch) (string, error) {
	return api.queries.GetGroupID(appID, trackName, arch)
}

// updateCachedGroups forwards to dbreads.InvalidateCachedGroups. Kept on
// *API so writer call sites (AddGroup/UpdateGroup/DeleteGroup) compile
// unchanged.
func (api *API) updateCachedGroups() {
	dbreads.InvalidateCachedGroups()
}

// GetGroupsCount forwards to api.queries.
func (api *API) GetGroupsCount(appID string) (int, error) {
	return api.queries.GetGroupsCount(appID)
}

// GetGroups forwards to api.queries.
func (api *API) GetGroups(appID string, page, perPage uint64) ([]*Group, error) {
	return api.queries.GetGroups(appID, page, perPage)
}

// getGroups forwards to api.queries.GetGroupsForApp. Used internally by
// applications.go's GetApp until that read also moves to dbreads.
func (api *API) getGroups(appID string) ([]*Group, error) {
	return api.queries.GetGroupsForApp(appID)
}

// validateChannel checks if a channel belongs to the application provided.
func (api *API) validateChannel(channelID, appID string) error {
	channel, err := api.GetChannel(channelID)
	if err != nil {
		return err
	}
	if channel.ApplicationID != appID {
		return ErrInvalidChannel
	}
	return nil
}

// getGroupUpdatesStats forwards to api.queries.GetGroupUpdatesStats. Kept
// lowercase on *API so existing runtime writers in events.go and updates.go
// compile unchanged.
func (api *API) getGroupUpdatesStats(group *Group) (*UpdatesStats, error) {
	return api.queries.GetGroupUpdatesStats(group)
}

// disableUpdates trips the safe-mode brake by setting the
// policy_updates_enabled override on the group_local row. The override
// lives on the node-local table, so it stops update grants on this node only.
func (api *API) disableUpdates(groupID string) error {
	query, _, err := goqu.Update("group_local").
		Set(goqu.Record{"policy_updates_enabled_override": false}).
		Where(goqu.C("group_id").Eq(groupID)).
		ToSQL()
	if err != nil {
		return err
	}
	_, err = api.db.Exec(query)

	return err
}

// setGroupRolloutInProgress updates the value of the rollout_in_progress flag
// for a given group, indicating if a rollout is taking place now or not.
func (api *API) setGroupRolloutInProgress(groupID string, inProgress bool) error {
	query, _, err := goqu.Update("group_local").
		Set(goqu.Record{"rollout_in_progress": inProgress}).
		Where(goqu.C("group_id").Eq(groupID)).
		ToSQL()
	if err != nil {
		return err
	}
	_, err = api.db.Exec(query)

	return err
}

// groupsQuery moved to pkg/api/dbreads/groups.go (q *Queries).

// GetGroupVersionBreakdown forwards to api.queries.
func (api *API) GetGroupVersionBreakdown(groupID string) ([]*VersionBreakdownEntry, error) {
	return api.queries.GetGroupVersionBreakdown(groupID)
}

// GetGroupInstancesStats forwards to api.queries.
func (api *API) GetGroupInstancesStats(groupID, duration string) (*InstancesStatusStats, error) {
	return api.queries.GetGroupInstancesStats(groupID, duration)
}
// duration helpers + isNightlyVersion + updateVersionTimeline moved to
// pkg/api/dbreads/groups.go.

// GetGroupVersionCountTimeline forwards to api.queries; TTL cache + 3-query
// implementation lives in pkg/api/dbreads/groups.go.
func (api *API) GetGroupVersionCountTimeline(groupID string, duration string) (map[time.Time](VersionCountMap), bool, error) {
	return api.queries.GetGroupVersionCountTimeline(groupID, duration)
}

// GetGroupStatusCountTimeline forwards to api.queries.
func (api *API) GetGroupStatusCountTimeline(groupID string, duration string) (map[time.Time](map[int](VersionCountMap)), error) {
	return api.queries.GetGroupStatusCountTimeline(groupID, duration)
}
