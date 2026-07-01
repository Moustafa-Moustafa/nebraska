package api

import (
	"errors"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/google/uuid"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/types"
)

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
	api.queries.UpdateCachedGroups()
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
	api.queries.UpdateCachedGroups()
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
	api.queries.UpdateCachedGroups()
	return nil
}

// GetGroup returns the group identified by the id provided.
func (api *API) GetGroup(groupID string) (*Group, error) {
	return api.queries.GetGroup(groupID)
}

// GetGroupID returns the ID of the first group identified by the track name and the channel architecture.
// The track names should be unique in combination with the group's channel architecture but this is not
// enforced on the DB level and the newest entry wins.
func (api *API) GetGroupID(appID, trackName string, arch Arch) (string, error) {
	return api.queries.GetGroupID(appID, trackName, arch)
}

// GetGroupsCount retuns the total number of groups in an app
func (api *API) GetGroupsCount(appID string) (int, error) {
	return api.queries.GetGroupsCount(appID)
}

// GetGroups returns all groups that belong to the application provided.
func (api *API) GetGroups(appID string, page, perPage uint64) ([]*Group, error) {
	return api.queries.GetGroups(appID, page, perPage)
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

// getGroupUpdatesStats returns a set of statistics about the distribution of
// updates and their status in the group provided.
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

// GetGroupVersionBreakdown returns a version breakdown of all instances running on a given group.
func (api *API) GetGroupVersionBreakdown(groupID string) ([]*VersionBreakdownEntry, error) {
	return api.queries.GetGroupVersionBreakdown(groupID)
}

// getGroupInstancesStats returns a summary of the status of the
// instances that belong to a given group.
func (api *API) GetGroupInstancesStats(groupID, duration string) (*InstancesStatusStats, error) {
	return api.queries.GetGroupInstancesStats(groupID, duration)
}

// This function computes instance version count form two different tables instance_application and instance_status_history.
// There are three types of instances that can exist.
// 1. Instances without any update history.
// 2. Instances which got updated in the duration(ie 30d,7d etc).
// 3. Instances that have updated but not in the duration.
// Here 1,3 doesn't contribute to growth or decline of the graph, they are straight lines in the graph.
// Based on this logic three queries are made concurrently and calculated to achieve the end result.
//
// Query 1 generates the time series using the `generate_series` postgres function and groups the
// instances without any update history(ie instance_application without any matching instance_status_history entry)
// based on version.
//
// Query 2 filters all instance_application with instance_status_history in the duration sorted desc by instance_id and created_ts
// So we have entries of instances_status_history based on the created_ts the count is increased for the corresponding versions in
// the corresponding spans programatically
//
// Query 3 filters all instance without any instance_status_history in the duration and takes the latest version for each instance and groups
// them to give a base count for all the versions. These version count values are directly added to all spans.
func (api *API) GetGroupVersionCountTimeline(groupID string, duration string) (map[time.Time](VersionCountMap), bool, error) {
	return api.queries.GetGroupVersionCountTimeline(groupID, duration)
}

func (api *API) GetGroupStatusCountTimeline(groupID string, duration string) (map[time.Time](map[int](VersionCountMap)), error) {
	return api.queries.GetGroupStatusCountTimeline(groupID, duration)
}
