package runtime

import (
	"time"

	"github.com/blang/semver/v4"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

// GetUpdatePackage returns an update package for the instance/application
// provided.
func (s *Service) GetUpdatePackage(instanceID, instanceAlias, instanceIP, instanceVersion, appID, groupID string) (*api.Package, error) {
	instance, err := s.RegisterInstance(instanceID, instanceAlias, instanceIP, instanceVersion, appID, groupID)
	if err != nil {
		l.Error().Err(err).Msg("GetUpdatePackage - could not register instance")
		return nil, api.ErrRegisterInstanceFailed
	}
	updateAlreadyGranted := false

	if instance.Application.Status.Valid {
		switch int(instance.Application.Status.Int64) {
		case api.InstanceStatusDownloading, api.InstanceStatusDownloaded, api.InstanceStatusInstalled:
			return nil, api.ErrUpdateInProgressOnInstance
		case api.InstanceStatusUpdateGranted:
			updateAlreadyGranted = true
		}
	}

	group, err := s.GetGroup(groupID)
	if err != nil {
		return nil, err
	}

	if group.Channel == nil || group.Channel.Package == nil {
		if err := s.newGroupActivityEntry(shared.ActivityPackageNotFound, shared.ActivityWarning, "0.0.0", appID, groupID); err != nil {
			l.Error().Err(err).Msg("GetUpdatePackage - could not add new group activity entry")
		}
		return nil, api.ErrNoPackageFound
	}

	for _, blacklistedChannelID := range group.Channel.Package.ChannelsBlacklist {
		if blacklistedChannelID == group.Channel.ID {
			if updateAlreadyGranted {
				if err := s.updateInstanceObjStatus(instance, api.InstanceStatusComplete); err != nil {
					l.Error().Err(err).Msg("GetUpdatePackage - could not update instance status")
				}
			}
			return nil, api.ErrNoUpdatePackageAvailable
		}
	}

	instanceSemver, _ := semver.Make(instanceVersion)
	packageSemver, _ := semver.Make(group.Channel.Package.Version)
	if !instanceSemver.LT(packageSemver) {
		if updateAlreadyGranted {
			if err := s.updateInstanceObjStatus(instance, api.InstanceStatusComplete); err != nil {
				l.Error().Err(err).Msg("GetUpdatePackage - could not update instance status")
			}
		}
		return nil, api.ErrNoUpdatePackageAvailable
	}

	if updateAlreadyGranted {
		return group.Channel.Package, nil
	}

	if err := s.enforceRolloutPolicy(instance, group); err != nil {
		return nil, err
	}

	version := group.Channel.Package.Version

	if err := s.grantUpdate(instance, version); err != nil {
		l.Error().Err(err).Msg("GetUpdatePackage - grantUpdate error")
	}

	if !s.HasRecentActivity(shared.ActivityRolloutStarted, api.ActivityQueryParams{Severity: shared.ActivityInfo, AppID: appID, Version: version, GroupID: group.ID}) {
		if err := s.newGroupActivityEntry(shared.ActivityRolloutStarted, shared.ActivityInfo, version, appID, group.ID); err != nil {
			l.Error().Err(err).Msg("GetUpdatePackage - could not add new group activity entry")
		}
	}

	if !group.RolloutInProgress {
		if err := s.setGroupRolloutInProgress(groupID, true); err != nil {
			l.Error().Err(err).Msg("GetUpdatePackage - could not set rollout progress")
		}
	}

	return group.Channel.Package, nil
}

func (s *Service) enforceRolloutPolicy(instance *api.Instance, group *api.Group) error {
	appID := instance.Application.ApplicationID

	if !group.PolicyUpdatesEnabled || group.SafeModeDisabled {
		return api.ErrUpdatesDisabled
	}

	if group.PolicyOfficeHours && !inOfficeHoursNow(group.PolicyTimezone.String) {
		return api.ErrUpdatesDisabled
	}

	effectiveMaxUpdates := group.PolicyMaxUpdatesPerPeriod

	if effectiveMaxUpdates >= maxParallelUpdates && !group.PolicySafeMode {
		return nil
	}

	updatesStats, err := s.GetGroupUpdatesStats(group)
	if err != nil {
		l.Error().Err(err).Msg("enforceRolloutPolicy - getGroupUpdatesStats error")
		return api.ErrGetUpdatesStatsFailed
	}

	if group.PolicySafeMode && updatesStats.UpdatesToCurrentVersionAttempted == 0 {
		effectiveMaxUpdates = 1
	}

	if updatesStats.UpdatesGrantedInLastPeriod >= effectiveMaxUpdates {
		if err := s.updateInstanceStatus(instance.ID, appID, api.InstanceStatusOnHold); err != nil {
			l.Error().Err(err).Msg("enforceRolloutPolicy - could not update instance status")
		}
		return api.ErrMaxUpdatesPerPeriodLimitReached
	}

	if updatesStats.UpdatesInProgress >= effectiveMaxUpdates {
		if err := s.updateInstanceStatus(instance.ID, appID, api.InstanceStatusOnHold); err != nil {
			l.Error().Err(err).Msg("enforceRolloutPolicy - could not update instance status")
		}
		return api.ErrMaxConcurrentUpdatesLimitReached
	}

	if group.PolicySafeMode && updatesStats.UpdatesTimedOut >= effectiveMaxUpdates {
		if group.PolicyUpdatesEnabled {
			if err := s.disableUpdates(group.ID); err != nil {
				l.Error().Err(err).Msg("enforceRolloutPolicy - could not disable updates")
			}
		}
		if err := s.updateInstanceStatus(instance.ID, appID, api.InstanceStatusOnHold); err != nil {
			l.Error().Err(err).Msg("enforceRolloutPolicy - could not update instance status")
		}
		return api.ErrMaxTimedOutUpdatesLimitReached
	}

	return nil
}

const maxParallelUpdates = 900000

func inOfficeHoursNow(tz string) bool {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return false
	}
	now := time.Now().In(loc)
	weekday := now.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false
	}
	hour := now.Hour()
	return hour >= 9 && hour < 17
}
