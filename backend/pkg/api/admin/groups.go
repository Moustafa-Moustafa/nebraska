package admin

import (
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/google/uuid"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/dbreads"
)

// isTimezoneValid checks if the provided timezone is valid.
func isTimezoneValid(tz string) bool {
	if tz == "" {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

// AddGroup registers the provided group.
func (s *Service) AddGroup(group *api.Group) (*api.Group, error) {
	if group.PolicyOfficeHours && !isTimezoneValid(group.PolicyTimezone.String) {
		return nil, api.ErrExpectingValidTimezone
	}

	if group.ChannelID.String != "" {
		if err := s.ValidateChannel(group.ChannelID.String, group.ApplicationID); err != nil {
			return nil, err
		}
	}
	if group.ID == "" {
		group.ID = uuid.New().String()
	}
	if group.Track == "" {
		group.Track = group.ID
	}
	query, _, err := goqu.Insert("groups").
		Cols("id", "name", "description", "application_id", "channel_id", "policy_updates_enabled",
			"policy_safe_mode", "policy_office_hours",
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
	err = s.db.QueryRowx(query).StructScan(group)
	if err != nil {
		return nil, err
	}

	dbreads.InvalidateCachedGroups()
	return group, nil
}

// UpdateGroup updates an existing group.
func (s *Service) UpdateGroup(group *api.Group) error {
	if group.PolicyOfficeHours && !isTimezoneValid(group.PolicyTimezone.String) {
		return api.ErrExpectingValidTimezone
	}

	groupBeforeUpdate, err := s.GetGroup(group.ID)
	if err != nil {
		return err
	}

	if group.ChannelID.String != "" {
		if err := s.ValidateChannel(group.ChannelID.String, groupBeforeUpdate.ApplicationID); err != nil {
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
	result, err := s.db.Exec(query)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return api.ErrNoRowsAffected
	}

	dbreads.InvalidateCachedGroups()
	return nil
}

// DeleteGroup removes the group identified by the id provided.
func (s *Service) DeleteGroup(groupID string) error {
	query, _, err := goqu.Delete("groups").Where(goqu.C("id").Eq(groupID)).ToSQL()
	if err != nil {
		return err
	}
	result, err := s.db.Exec(query)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return api.ErrNoRowsAffected
	}
	dbreads.InvalidateCachedGroups()
	return nil
}
