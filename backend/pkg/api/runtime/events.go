package runtime

import (
	"github.com/doug-martin/goqu/v9"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

// RegisterEvent registers an event posted by an instance in Nebraska.
func (s *Service) RegisterEvent(instanceID, appID, groupID string, etype, eresult int, previousVersion, errorCode string) error {
	var err error
	if appID, groupID, err = s.validateApplicationAndGroup(appID, groupID); err != nil {
		return err
	}
	instance, err := s.GetInstance(instanceID, appID)
	if err != nil {
		l.Info().Err(err).Msg("RegisterEvent - could not get instance")
		return api.ErrInvalidInstance
	}
	if instance.Application.ApplicationID != appID {
		return api.ErrInvalidApplicationOrGroup
	}
	if !instance.Application.UpdateInProgress {
		return api.ErrNoUpdateInProgress
	}

	// Temporary hack for Flatcar updater specific behaviour
	if appID == api.FlatcarAppID && etype == api.EventUpdateComplete && eresult == api.ResultSuccessReboot {
		if previousVersion == "" || previousVersion == "0.0.0.0" {
			if err := s.updateInstanceObjStatus(instance, api.InstanceStatusUndefined); err != nil {
				l.Error().Err(err).Msg("RegisterEvent - could not update instance status")
			}
			return api.ErrFlatcarEventIgnored
		}
	}

	var eventTypeID int
	query, _, err := goqu.From("event_type").
		Select("id").
		Where(goqu.C("type").Eq(etype), goqu.C("result").Eq(eresult)).
		ToSQL()
	if err != nil {
		return err
	}
	err = s.db.QueryRow(query).Scan(&eventTypeID)
	if err != nil {
		return api.ErrInvalidEventTypeOrResult
	}

	insertQuery, _, err := goqu.Insert("event").
		Cols("event_type_id", "instance_id", "application_id", "previous_version", "error_code").
		Vals(goqu.Vals{eventTypeID, instanceID, appID, previousVersion, errorCode}).
		ToSQL()
	if err != nil {
		return err
	}
	if _, err = s.db.Exec(insertQuery); err != nil {
		return api.ErrEventRegistrationFailed
	}

	lastUpdateVersion := instance.Application.LastUpdateVersion.String
	if err := s.triggerEventConsequences(instanceID, appID, groupID, lastUpdateVersion, etype, eresult); err != nil {
		l.Error().Err(err).Msgf("RegisterEvent - could not trigger event consequences")
	}

	return nil
}

func (s *Service) triggerEventConsequences(instanceID, appID, groupID, lastUpdateVersion string, etype, result int) error {
	group, err := s.GetGroup(groupID)
	if err != nil {
		return err
	}

	if etype == api.EventUpdateComplete && (result == api.ResultSuccessReboot || (appID != api.FlatcarAppID && result == api.ResultSuccess)) {
		if err := s.updateInstanceStatus(instanceID, appID, api.InstanceStatusComplete); err != nil {
			l.Error().Err(err).Msg("triggerEventConsequences - could not update instance status")
		}

		updatesStats, err := s.GetGroupUpdatesStats(group)
		if err != nil {
			return err
		}
		if updatesStats.UpdatesToCurrentVersionSucceeded == updatesStats.TotalInstances {
			if err := s.setGroupRolloutInProgress(groupID, false); err != nil {
				l.Error().Err(err).Msg("triggerEventConsequences - could not set rollout progress")
			}
			if err := s.newGroupActivityEntry(shared.ActivityRolloutFinished, shared.ActivitySuccess, lastUpdateVersion, appID, groupID); err != nil {
				l.Error().Err(err).Msg("triggerEventConsequences - could not add group activity")
			}
		}
	}

	if etype == api.EventUpdateDownloadStarted && result == api.ResultSuccess {
		if err := s.updateInstanceStatus(instanceID, appID, api.InstanceStatusDownloading); err != nil {
			l.Error().Err(err).Msg("triggerEventConsequences - could not update instance status")
		}
	}

	if etype == api.EventUpdateDownloadFinished && result == api.ResultSuccess {
		if err := s.updateInstanceStatus(instanceID, appID, api.InstanceStatusDownloaded); err != nil {
			l.Error().Err(err).Msg("triggerEventConsequences - could not update instance status")
		}
	}

	if etype == api.EventUpdateInstalled && result == api.ResultSuccess {
		if err := s.updateInstanceStatus(instanceID, appID, api.InstanceStatusInstalled); err != nil {
			l.Error().Err(err).Msg("triggerEventConsequences - could not update instance status")
		}
	}

	if result == api.ResultFailed {
		if err := s.updateInstanceStatus(instanceID, appID, api.InstanceStatusError); err != nil {
			l.Error().Err(err).Msg("triggerEventConsequences - could not update instance status")
		}
		if err := s.newInstanceActivityEntry(shared.ActivityInstanceUpdateFailed, shared.ActivityError, lastUpdateVersion, appID, groupID, instanceID); err != nil {
			l.Error().Err(err).Msg("triggerEventConsequences - could not add instance activity")
		}

		if s.disableUpdatesOnFailedRollout {
			updatesStats, err := s.GetGroupUpdatesStats(group)
			if err != nil {
				return err
			}
			if updatesStats.UpdatesToCurrentVersionAttempted == 1 {
				if err := s.disableUpdates(groupID); err != nil {
					l.Error().Err(err).Msg("triggerEventConsequences - could not disable updates")
				}
				if err := s.setGroupRolloutInProgress(groupID, false); err != nil {
					l.Error().Err(err).Msg("triggerEventConsequences - could not set rollout progress")
				}
				if err := s.newGroupActivityEntry(shared.ActivityRolloutFailed, shared.ActivityError, lastUpdateVersion, appID, groupID); err != nil {
					l.Error().Err(err).Msg("triggerEventConsequences - could not add group activity")
				}
			}
		}
	}

	return nil
}
