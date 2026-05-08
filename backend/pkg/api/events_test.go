package api_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"gopkg.in/guregu/null.v4"

	"github.com/flatcar/nebraska/backend/pkg/api"
)

func TestRegisterEvent_InvalidParams(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup, _ := adminSvc(a).AddGroup(&api.Group{Name: "group1", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	tInstance, _ := runtimeSvc(a).RegisterInstance(uuid.New().String(), "", "10.0.0.1", "1.0.0", tApp.ID, tGroup.ID)

	err := runtimeSvc(a).RegisterEvent(uuid.New().String(), tApp.ID, tGroup.ID, api.EventUpdateComplete, api.ResultSuccessReboot, "", "")
	assert.Equal(t, api.ErrInvalidInstance, err)

	err = runtimeSvc(a).RegisterEvent(tInstance.ID, uuid.New().String(), tGroup.ID, api.EventUpdateComplete, api.ResultSuccessReboot, "", "")
	assert.Equal(t, api.ErrInvalidApplicationOrGroup, err)

	err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, uuid.New().String(), api.EventUpdateComplete, api.ResultSuccessReboot, "", "")
	assert.Equal(t, sql.ErrNoRows, err)

	err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, tGroup.ID, api.EventUpdateDownloadStarted, api.ResultSuccess, "", "")
	assert.Equal(t, api.ErrNoUpdateInProgress, err)

	_, _ = runtimeSvc(a).GetUpdatePackage(tInstance.ID, "", "10.0.0.1", "1.0.0", tApp.ID, tGroup.ID)

	err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, tGroup.ID, 1000, api.ResultSuccess, "", "")
	assert.Equal(t, api.ErrInvalidEventTypeOrResult, err)

	err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, tGroup.ID, api.EventUpdateComplete, 1000, "", "")
	assert.Equal(t, api.ErrInvalidEventTypeOrResult, err)
}

func TestRegisterEvent_TriggerEventConsequences(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup, _ := adminSvc(a).AddGroup(&api.Group{Name: "group1", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	tInstance, _ := runtimeSvc(a).RegisterInstance(uuid.New().String(), "", "10.0.0.1", "1.0.0", tApp.ID, tGroup.ID)
	tInstance2, _ := runtimeSvc(a).RegisterInstance(uuid.New().String(), "", "10.0.0.2", "1.0.0", tApp.ID, tGroup.ID)

	_, err := runtimeSvc(a).GetUpdatePackage(tInstance.ID, "", "10.0.0.1", "12.0.0", tApp.ID, tGroup.ID)
	assert.NoError(t, err)

	err = runtimeSvc(a).RegisterEvent(tInstance.ID, "{"+tApp.ID+"}", tGroup.ID, api.EventUpdateDownloadStarted, api.ResultSuccess, "", "")
	assert.NoError(t, err)
	instance, _ := runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
	assert.Equal(t, null.IntFrom(int64(api.InstanceStatusDownloading)), instance.Application.Status)

	err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, "{"+tGroup.ID+"}", api.EventUpdateDownloadFinished, api.ResultSuccess, "", "")
	assert.NoError(t, err)
	instance, _ = runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
	assert.Equal(t, null.IntFrom(int64(api.InstanceStatusDownloaded)), instance.Application.Status)

	err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, tGroup.ID, api.EventUpdateInstalled, api.ResultSuccess, "", "")
	assert.NoError(t, err)
	instance, _ = runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
	assert.Equal(t, null.IntFrom(int64(api.InstanceStatusInstalled)), instance.Application.Status)

	err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, tGroup.ID, api.EventUpdateComplete, api.ResultSuccessReboot, "", "")
	assert.NoError(t, err)
	instance, _ = runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
	assert.Equal(t, null.IntFrom(int64(api.InstanceStatusComplete)), instance.Application.Status)

	_, err = runtimeSvc(a).GetUpdatePackage(tInstance2.ID, "", "10.0.0.2", "12.0.0", tApp.ID, tGroup.ID)
	assert.NoError(t, err)

	err = runtimeSvc(a).RegisterEvent(tInstance2.ID, tApp.ID, tGroup.ID, api.EventUpdateComplete, api.ResultFailed, "", "")
	assert.NoError(t, err)
	instance, _ = runtimeSvc(a).GetInstance(tInstance2.ID, tApp.ID)
	assert.Equal(t, null.IntFrom(int64(api.InstanceStatusError)), instance.Application.Status)
	group, _ := runtimeSvc(a).GetGroup(tGroup.ID)
	assert.Equal(t, true, group.PolicyUpdatesEnabled, "It wasn't the first update the one that failed.")
}

func TestRegisterEvent_TriggerEventConsequences_FirstUpdateAttemptFailed(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup, _ := adminSvc(a).AddGroup(&api.Group{Name: "group1", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	tInstance, _ := runtimeSvc(a).RegisterInstance(uuid.New().String(), "", "10.0.0.1", "1.0.0", tApp.ID, tGroup.ID)

	_, err := runtimeSvc(a).GetUpdatePackage(tInstance.ID, "", "10.0.0.1", "12.0.0", tApp.ID, tGroup.ID)
	assert.NoError(t, err)

	err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, tGroup.ID, api.EventUpdateComplete, api.ResultFailed, "", "")
	assert.NoError(t, err)
	instance, _ := runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
	assert.Equal(t, null.IntFrom(int64(api.InstanceStatusError)), instance.Application.Status)
	group, _ := runtimeSvc(a).GetGroup(tGroup.ID)
	assert.Equal(t, true, group.SafeModeDisabled, "First update attempt failed, safe mode should be disabled.")
}

func TestRegisterEvent_CheckSuccessResult(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	performUpdate := func(tApp *api.Application, tGroup *api.Group, resultType int) {
		tInstance, err := runtimeSvc(a).RegisterInstance(uuid.New().String(), "", "10.0.0.1", "1.0.0", tApp.ID, tGroup.ID)
		assert.NoError(t, err)

		_, err = runtimeSvc(a).GetUpdatePackage(tInstance.ID, "", "10.0.0.1", "12.0.0", tApp.ID, tGroup.ID)
		assert.NoError(t, err)

		err = runtimeSvc(a).RegisterEvent(tInstance.ID, "{"+tApp.ID+"}", tGroup.ID, api.EventUpdateDownloadStarted, api.ResultSuccess, "", "")
		assert.NoError(t, err)
		instance, _ := runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
		assert.Equal(t, null.IntFrom(int64(api.InstanceStatusDownloading)), instance.Application.Status)

		err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, "{"+tGroup.ID+"}", api.EventUpdateDownloadFinished, api.ResultSuccess, "", "")
		assert.NoError(t, err)
		instance, _ = runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
		assert.Equal(t, null.IntFrom(int64(api.InstanceStatusDownloaded)), instance.Application.Status)

		err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, tGroup.ID, api.EventUpdateInstalled, api.ResultSuccess, "", "")
		assert.NoError(t, err)
		instance, _ = runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
		assert.Equal(t, null.IntFrom(int64(api.InstanceStatusInstalled)), instance.Application.Status)

		err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, tGroup.ID, api.EventUpdateComplete, resultType, "", "")
		assert.NoError(t, err)
		instance, _ = runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
		assert.Equal(t, null.IntFrom(int64(api.InstanceStatusComplete)), instance.Application.Status)
	}

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup, _ := adminSvc(a).AddGroup(&api.Group{Name: "group1", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})

	performUpdate(tApp, tGroup, api.ResultSuccess)
	performUpdate(tApp, tGroup, api.ResultSuccessReboot)
}

func TestRegisterEvent_CheckFlatcarSuccessResult(t *testing.T) {
	// If it's a Flatcar application, then the instances' updates are only considered to
	// be complete if the instance has sent ResultSuccessReboot on completion.
	a := newForTest(t)
	defer a.Close()

	performUpdate := func(tApp *api.Application, tGroup *api.Group, resultType, expectedInstanceStatus int) {
		tInstance, err := runtimeSvc(a).RegisterInstance(uuid.New().String(), "", "10.0.0.1", "1.0.0", tApp.ID, tGroup.ID)
		assert.NoError(t, err)

		_, err = runtimeSvc(a).GetUpdatePackage(tInstance.ID, "", "10.0.0.1", "12.0.0", tApp.ID, tGroup.ID)
		assert.NoError(t, err)

		err = runtimeSvc(a).RegisterEvent(tInstance.ID, "{"+tApp.ID+"}", tGroup.ID, api.EventUpdateDownloadStarted, api.ResultSuccess, "11.0.0", "")
		assert.NoError(t, err)
		instance, _ := runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
		assert.Equal(t, null.IntFrom(int64(api.InstanceStatusDownloading)), instance.Application.Status)

		err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, "{"+tGroup.ID+"}", api.EventUpdateDownloadFinished, api.ResultSuccess, "11.0.0", "")
		assert.NoError(t, err)
		instance, _ = runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
		assert.Equal(t, null.IntFrom(int64(api.InstanceStatusDownloaded)), instance.Application.Status)

		err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, tGroup.ID, api.EventUpdateInstalled, api.ResultSuccess, "11.0.0", "")
		assert.NoError(t, err)
		instance, _ = runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
		assert.Equal(t, null.IntFrom(int64(api.InstanceStatusInstalled)), instance.Application.Status)

		err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, tGroup.ID, api.EventUpdateComplete, resultType, "11.0.0", "")
		assert.NoError(t, err)
		instance, _ = runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
		assert.Equal(t, null.IntFrom(int64(expectedInstanceStatus)), instance.Application.Status)
	}

	tApp, _ := runtimeSvc(a).GetApp(api.FlatcarAppID)
	tPkg, _ := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup, _ := adminSvc(a).AddGroup(&api.Group{Name: "group9", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: false, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})

	performUpdate(tApp, tGroup, api.ResultSuccess, api.InstanceStatusInstalled)
	performUpdate(tApp, tGroup, api.ResultSuccessReboot, api.InstanceStatusComplete)
}

func TestRegisterEvent_CheckFlatcarIgnoredUpdate(t *testing.T) {
	// If it's a Flatcar application, and the instance reports that it updated to "" or 0.0.0.0 as the version,
	// then the event is ignored.
	a := newForTest(t)
	defer a.Close()

	tApp, _ := runtimeSvc(a).GetApp(api.FlatcarAppID)
	tPkg, _ := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup, _ := adminSvc(a).AddGroup(&api.Group{Name: "group9", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: false, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})

	performUpdate := func(previousVersion string) {
		tInstance, err := runtimeSvc(a).RegisterInstance(uuid.New().String(), "", "10.0.0.1", "1.0.0", tApp.ID, tGroup.ID)
		assert.NoError(t, err)

		_, err = runtimeSvc(a).GetUpdatePackage(tInstance.ID, "", "10.0.0.1", "12.0.0", tApp.ID, tGroup.ID)
		assert.NoError(t, err)

		err = runtimeSvc(a).RegisterEvent(tInstance.ID, "{"+tApp.ID+"}", tGroup.ID, api.EventUpdateDownloadStarted, api.ResultSuccess, previousVersion, "")
		assert.NoError(t, err)
		instance, _ := runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
		assert.Equal(t, null.IntFrom(int64(api.InstanceStatusDownloading)), instance.Application.Status)

		err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, "{"+tGroup.ID+"}", api.EventUpdateDownloadFinished, api.ResultSuccess, previousVersion, "")
		assert.NoError(t, err)
		instance, _ = runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
		assert.Equal(t, null.IntFrom(int64(api.InstanceStatusDownloaded)), instance.Application.Status)

		err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, tGroup.ID, api.EventUpdateInstalled, api.ResultSuccess, previousVersion, "")
		assert.NoError(t, err)
		instance, _ = runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
		assert.Equal(t, null.IntFrom(int64(api.InstanceStatusInstalled)), instance.Application.Status)

		err = runtimeSvc(a).RegisterEvent(tInstance.ID, tApp.ID, tGroup.ID, api.EventUpdateComplete, api.ResultSuccessReboot, previousVersion, "")
		assert.Error(t, err, "Received unexpected error: \nnebraska: flatcar event ignored")
		instance, _ = runtimeSvc(a).GetInstance(tInstance.ID, tApp.ID)
		assert.Equal(t, null.IntFrom(int64(api.InstanceStatusUndefined)), instance.Application.Status)
	}

	performUpdate("0.0.0.0")
	performUpdate("")
}

func TestRegisterEvent_GetEvent(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup, _ := adminSvc(a).AddGroup(&api.Group{Name: "group1", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	tInstance, _ := runtimeSvc(a).RegisterInstance(uuid.New().String(), "", "10.0.0.1", "1.0.0", tApp.ID, tGroup.ID)

	_, err := runtimeSvc(a).GetUpdatePackage(tInstance.ID, "", "10.0.0.1", "12.0.0", tApp.ID, tGroup.ID)
	assert.NoError(t, err)

	_, err = runtimeSvc(a).GetEvent(tInstance.ID, tApp.ID, time.Now())
	assert.Error(t, err, "sql: no rows in result set")

	err = runtimeSvc(a).RegisterEvent(tInstance.ID, "{"+tApp.ID+"}", tGroup.ID, api.EventUpdateDownloadStarted, api.ResultSuccess, "", "")
	assert.NoError(t, err)

	errCode, err := runtimeSvc(a).GetEvent(tInstance.ID, tApp.ID, time.Now())
	assert.NoError(t, err)
	assert.Equal(t, errCode, null.StringFrom(""))

	err = runtimeSvc(a).RegisterEvent(tInstance.ID, "{"+tApp.ID+"}", tGroup.ID, api.EventUpdateDownloadFinished, api.ResultSuccess, "", "")
	assert.NoError(t, err)

	errCode, err = runtimeSvc(a).GetEvent(tInstance.ID, tApp.ID, time.Now())
	assert.NoError(t, err)
	assert.Equal(t, errCode, null.StringFrom(""))
}
