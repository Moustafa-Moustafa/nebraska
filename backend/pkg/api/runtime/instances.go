package runtime

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/google/uuid"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

// RegisterInstance registers an instance into Nebraska.
func (s *Service) RegisterInstance(instanceID, instanceAlias, instanceIP, instanceVersion, appID, groupID string) (*api.Instance, error) {
	if !shared.IsValidSemver(instanceVersion) {
		return nil, api.ErrInvalidSemver
	}
	var err error
	if appID, groupID, err = s.validateApplicationAndGroup(appID, groupID); err != nil {
		return nil, err
	}

	updateInstance := true
	updateInstanceApplication := true

	instance, err := s.GetInstance(instanceID, appID)
	if err == nil {
		if instanceAlias == "" {
			instanceAlias = instance.Alias
		}
		updateInstance = instance.IP != instanceIP || instance.Alias != instanceAlias

		recent := time.Now().UTC().Add(-5 * time.Minute)
		updateInstanceApplication = instance.Application.LastCheckForUpdates.UTC().Before(recent) ||
			instance.Application.Version != instanceVersion || instance.Application.GroupID.String != groupID

		if !updateInstance && !updateInstanceApplication {
			return instance, nil
		}
	}

	now := time.Now().UTC()

	upsertInstance, _, err := goqu.Insert("instance").
		Cols("id", "ip", "alias").
		Vals(goqu.Vals{instanceID, instanceIP, instanceAlias}).
		OnConflict(goqu.DoUpdate("id", goqu.Record{"id": instanceID, "ip": instanceIP, "alias": instanceAlias})).
		ToSQL()
	if err != nil {
		return nil, err
	}

	upsertInstanceApplication, _, err := goqu.Insert("instance_application").
		Cols("instance_id", "application_id", "group_id", "version", "last_check_for_updates").
		Vals(goqu.Vals{instanceID, appID, groupID, instanceVersion, now}).
		OnConflict(goqu.DoUpdate("ON CONSTRAINT instance_application_pkey", goqu.Record{"group_id": groupID, "version": instanceVersion, "last_check_for_updates": now})).
		ToSQL()
	if err != nil {
		return nil, err
	}

	if updateInstance != updateInstanceApplication {
		queryToExec := upsertInstance
		if updateInstanceApplication {
			queryToExec = upsertInstanceApplication
		}
		if _, err := s.db.Exec(queryToExec); err != nil {
			return nil, err
		}
		return instance, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			l.Error().Err(err).Msg("RegisterInstance - could not roll back")
		}
	}()

	result, err := tx.Exec(upsertInstance)
	if err != nil {
		return nil, err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return nil, fmt.Errorf("RegisterInstance instance insert failed")
	}

	result, err = tx.Exec(upsertInstanceApplication)
	if err != nil {
		return nil, err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return nil, fmt.Errorf("RegisterInstance upsert for instance_application failed")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetInstance(instanceID, appID)
}

// UpdateInstance updates an instance's alias.
func (s *Service) UpdateInstance(instanceID string, alias string) (*api.Instance, error) {
	var instance api.Instance
	query, _, err := goqu.Update("instance").
		Set(goqu.Record{"alias": alias}).
		Where(goqu.C("id").Eq(instanceID)).
		Returning(goqu.T("instance").All()).
		ToSQL()
	if err != nil {
		return nil, err
	}
	err = s.db.QueryRowx(query).StructScan(&instance)
	if err != nil {
		return nil, err
	}
	return &instance, nil
}

func (s *Service) validateApplicationAndGroup(appID, groupID string) (string, string, error) {
	appUUID, err := uuid.Parse(appID)
	if err != nil {
		return "", "", err
	}
	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		return "", "", err
	}

	group, err := s.GetGroup(groupID)
	if err != nil {
		return "", "", err
	}

	if group.ApplicationID != appUUID.String() {
		return "", "", api.ErrInvalidApplicationOrGroup
	}

	return appUUID.String(), groupUUID.String(), nil
}

func (s *Service) updateInstanceStatus(instanceID, appID string, newStatus int) error {
	instance, err := s.GetInstance(instanceID, appID)
	if err != nil {
		return err
	}
	return s.updateInstanceObjStatus(instance, newStatus)
}

func (s *Service) grantUpdate(instance *api.Instance, version string) error {
	insertData := make(map[string]interface{})
	insertData["last_update_granted_ts"] = time.Now().UTC()
	insertData["last_update_version"] = version
	insertData["status"] = api.InstanceStatusUpdateGranted
	insertData["update_in_progress"] = true
	return s.updateInstanceData(instance, insertData)
}

func (s *Service) updateInstanceData(instance *api.Instance, data map[string]interface{}) error {
	appID := instance.Application.ApplicationID
	insertData := data
	newStatus := insertData["status"].(int)

	if instance.Application.Status.Valid && instance.Application.Status.Int64 == int64(newStatus) {
		return nil
	}

	if newStatus == api.InstanceStatusComplete {
		insertData["version"] = goqu.L("CASE WHEN last_update_version IS NOT NULL THEN last_update_version ELSE version END")
	}

	if newStatus == api.InstanceStatusComplete || newStatus == api.InstanceStatusError || newStatus == api.InstanceStatusUndefined || newStatus == api.InstanceStatusOnHold {
		insertData["update_in_progress"] = false
	}

	insertQuery, _, err := goqu.Insert("instance_status_history").
		Cols("status", "version", "instance_id", "application_id", "group_id").
		With("inst_app", goqu.Update("instance_application").
			Set(insertData).
			Where(goqu.C("instance_id").Eq(instance.ID), goqu.C("application_id").Eq(appID)).
			Returning("instance_id", "application_id", "last_update_version", "group_id")).
		FromQuery(goqu.From(goqu.L("inst_app")).
			Select(goqu.V(newStatus).As("status"), goqu.C("last_update_version").As("version"), goqu.C("instance_id"), goqu.C("application_id"), goqu.C("group_id"))).
		ToSQL()
	if err != nil {
		return err
	}

	_, err = s.db.Exec(insertQuery)
	return err
}

func (s *Service) updateInstanceObjStatus(instance *api.Instance, newStatus int) error {
	insertData := make(map[string]interface{})
	insertData["status"] = newStatus
	return s.updateInstanceData(instance, insertData)
}

// Exported wrappers for test access

func (s *Service) GrantUpdate(instance *api.Instance, version string) error {
	return s.grantUpdate(instance, version)
}

func (s *Service) UpdateInstanceStatus(instanceID, appID string, newStatus int) error {
	return s.updateInstanceStatus(instanceID, appID, newStatus)
}

// UpdateInstanceStats inserts a snapshot of instance counts into
// instance_stats, grouped by channel, arch and version.
func (s *Service) UpdateInstanceStats(t *time.Time, duration *time.Duration) error {
	insertQuery, _, err := goqu.Insert(goqu.T("instance_stats")).
		Cols("timestamp", "channel_name", "arch", "version", "instances").
		FromQuery(s.instanceStatsQuery(t, duration)).
		ToSQL()
	if err != nil {
		return err
	}
	_, err = s.db.Exec(insertQuery)
	return err
}

const defaultStatsInterval = 2 * time.Hour

func (s *Service) instanceStatsQuery(t *time.Time, duration *time.Duration) *goqu.SelectDataset {
	if t == nil {
		now := time.Now().UTC()
		t = &now
	}
	if duration == nil {
		d := defaultStatsInterval
		duration = &d
	}
	durationToInterval := func(d time.Duration) string {
		if d <= 0 {
			d = time.Microsecond
		}
		parts := []string{}
		hours := int(d.Hours())
		if hours != 0 {
			parts = append(parts, fmt.Sprintf("%d hours", hours))
		}
		remainder := d - time.Duration(hours)*time.Hour
		minutes := int(remainder.Minutes())
		if minutes != 0 {
			parts = append(parts, fmt.Sprintf("%d minutes", minutes))
		}
		remainder -= time.Duration(minutes) * time.Minute
		seconds := int(remainder.Seconds())
		if seconds != 0 {
			parts = append(parts, fmt.Sprintf("%d seconds", seconds))
		}
		remainder -= time.Duration(seconds) * time.Second
		microseconds := remainder.Microseconds()
		if microseconds != 0 {
			parts = append(parts, fmt.Sprintf("%d microseconds", microseconds))
		}
		return strings.Join(parts, " ")
	}

	interval := durationToInterval(*duration)
	timestamp := goqu.L("timestamp ?", goqu.V(t.Format("2006-01-02T15:04:05.999999Z07:00")))
	timestampMinusDuration := goqu.L("timestamp ? - interval ?", goqu.V(t.Format("2006-01-02T15:04:05.999999Z07:00")), interval)

	return goqu.From(goqu.T("instance_application")).
		Select(
			timestamp,
			goqu.T("channel").Col("name").As("channel_name"),
			goqu.Case().
				When(goqu.T("channel").Col("arch").Eq(1), "AMD64").
				When(goqu.T("channel").Col("arch").Eq(2), "ARM").
				Else("").
				As("arch"),
			goqu.C("version").As("version"),
			goqu.COUNT("*").As("instances")).
		Join(goqu.T("groups"), goqu.On(goqu.C("group_id").Eq(goqu.T("groups").Col("id")))).
		Join(goqu.T("channel"), goqu.On(goqu.T("groups").Col("channel_id").Eq(goqu.T("channel").Col("id")))).
		Where(
			goqu.C("last_check_for_updates").Gt(timestampMinusDuration),
			goqu.C("last_check_for_updates").Lte(timestamp)).
		GroupBy(timestamp,
			goqu.T("channel").Col("name"),
			goqu.T("channel").Col("arch"),
			goqu.C("version")).
		Order(timestamp.Asc())
}
