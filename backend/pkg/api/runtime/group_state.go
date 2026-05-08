package runtime

import (
	"github.com/doug-martin/goqu/v9"
)

// disableUpdates sets safe_mode_disabled to true in group_state.
// This is the runtime's emergency brake when a rollout fails in safe mode,
// the runtime disables further updates per-region. The admin's
// policy_updates_enabled (on the replicated groups table) is a separate,
// global control.
func (s *Service) disableUpdates(groupID string) error {
	query, _, err := goqu.Update("group_state").
		Set(goqu.Record{"safe_mode_disabled": true}).
		Where(goqu.C("group_id").Eq(groupID)).
		ToSQL()
	if err != nil {
		return err
	}
	_, err = s.db.Exec(query)
	return err
}

// setGroupRolloutInProgress updates the rollout_in_progress flag in group_state.
func (s *Service) setGroupRolloutInProgress(groupID string, inProgress bool) error {
	query, _, err := goqu.Update("group_state").
		Set(goqu.Record{"rollout_in_progress": inProgress}).
		Where(goqu.C("group_id").Eq(groupID)).
		ToSQL()
	if err != nil {
		return err
	}
	_, err = s.db.Exec(query)
	return err
}

// newGroupActivityEntry creates a new activity entry for a group.
func (s *Service) newGroupActivityEntry(class int, severity int, version, appID, groupID string) error {
	query, _, err := goqu.Insert("activity").
		Cols("class", "severity", "version", "application_id", "group_id").
		Vals(goqu.Vals{class, severity, version, appID, groupID}).
		ToSQL()
	if err != nil {
		return err
	}
	_, err = s.db.Exec(query)
	return err
}

// newInstanceActivityEntry creates a new activity entry for an instance.
func (s *Service) newInstanceActivityEntry(class int, severity int, version, appID, groupID, instanceID string) error {
	query, _, err := goqu.Insert("activity").
		Cols("class", "severity", "version", "application_id", "group_id", "instance_id").
		Vals(goqu.Vals{class, severity, version, appID, groupID, instanceID}).
		ToSQL()
	if err != nil {
		return err
	}
	_, err = s.db.Exec(query)
	return err
}

// Exported wrappers for test access

func (s *Service) NewGroupActivityEntry(class int, severity int, version, appID, groupID string) error {
	return s.newGroupActivityEntry(class, severity, version, appID, groupID)
}

func (s *Service) NewInstanceActivityEntry(class int, severity int, version, appID, groupID, instanceID string) error {
	return s.newInstanceActivityEntry(class, severity, version, appID, groupID, instanceID)
}
