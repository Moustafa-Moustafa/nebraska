package api_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"gopkg.in/guregu/null.v4"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

func TestGetActivity(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tVersion := "12.1.0"
	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: tVersion, ApplicationID: tApp.ID})
	tChannel, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup, _ := adminSvc(a).AddGroup(&api.Group{Name: "group1", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	tGroup2, _ := adminSvc(a).AddGroup(&api.Group{Name: "group2", ApplicationID: tApp.ID, PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	tInstance, _ := runtimeSvc(a).RegisterInstance(uuid.New().String(), "", "10.0.0.1", "1.0.0", tApp.ID, tGroup.ID)
	tInstance2, _ := runtimeSvc(a).RegisterInstance(uuid.New().String(), "", "10.0.0.2", "1.0.0", tApp.ID, tGroup2.ID)
	tFakeInstance, _ := runtimeSvc(a).RegisterInstance("{"+uuid.New().String()+"}", "", "10.0.0.2", "1.0.0", tApp.ID, tGroup2.ID)

	_ = runtimeSvc(a).NewGroupActivityEntry(shared.ActivityRolloutStarted, shared.ActivitySuccess, tVersion, tApp.ID, tGroup.ID)
	_ = runtimeSvc(a).NewGroupActivityEntry(shared.ActivityRolloutStarted, shared.ActivitySuccess, tVersion, tApp.ID, tGroup2.ID)
	_ = runtimeSvc(a).NewInstanceActivityEntry(shared.ActivityInstanceUpdateFailed, shared.ActivityError, tVersion, tApp.ID, tGroup.ID, tInstance.ID)
	_ = runtimeSvc(a).NewInstanceActivityEntry(shared.ActivityInstanceUpdateFailed, shared.ActivityError, tVersion, tApp.ID, tGroup2.ID, tInstance2.ID)
	_ = runtimeSvc(a).NewGroupActivityEntry(shared.ActivityInstanceUpdateFailed, shared.ActivitySuccess, tVersion, tApp.ID, tGroup.ID)
	_ = runtimeSvc(a).NewInstanceActivityEntry(shared.ActivityInstanceUpdateFailed, shared.ActivityError, tVersion, tApp.ID, tGroup.ID, tFakeInstance.ID)

	time.Sleep(10 * time.Millisecond)

	// this should ignore the entry for the fake instance
	activityEntries, err := runtimeSvc(a).GetActivity(tTeam.ID, api.ActivityQueryParams{AppID: tApp.ID, GroupID: tGroup.ID})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(activityEntries))

	activityEntries, err = runtimeSvc(a).GetActivity(tTeam.ID, api.ActivityQueryParams{Severity: shared.ActivityError})
	assert.NoError(t, err)
	assert.Equal(t, 2, len(activityEntries))

	activityEntries, err = runtimeSvc(a).GetActivity(tTeam.ID, api.ActivityQueryParams{InstanceID: tInstance2.ID})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(activityEntries))

	// when asked explicitly, fake instance won't be ignored
	activityEntries, err = runtimeSvc(a).GetActivity(tTeam.ID, api.ActivityQueryParams{InstanceID: tFakeInstance.ID})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(activityEntries))

	activityEntries, err = runtimeSvc(a).GetActivity(tTeam.ID, api.ActivityQueryParams{})
	assert.NoError(t, err)
	assert.Equal(t, 5, len(activityEntries))
	anActivity := activityEntries[0]

	hasRecentActivity := runtimeSvc(a).HasRecentActivity(shared.ActivityInstanceUpdateFailed, api.ActivityQueryParams{Severity: shared.ActivitySuccess, AppID: tApp.ID, Version: tVersion, GroupID: tGroup.ID})
	assert.True(t, hasRecentActivity)

	_, err = runtimeSvc(a).GetActivity("invalidTeamID", api.ActivityQueryParams{})
	assert.Error(t, err, "Team id used must be a valid uuid.")

	activityEntries, err = runtimeSvc(a).GetActivity(uuid.New().String(), api.ActivityQueryParams{})
	assert.NoError(t, err)
	assert.Nil(t, activityEntries, "Team with this id doesn't exist")

	// We try counting with default Start==-3days, End==Now
	totalCount, err := runtimeSvc(a).GetActivityCount(tTeam.ID, api.ActivityQueryParams{})
	assert.NoError(t, err)
	assert.Equal(t, 5, totalCount)

	totalCount, err = runtimeSvc(a).GetActivityCount(tTeam.ID,
		api.ActivityQueryParams{
			Start: anActivity.CreatedTs.Add(time.Duration(-10) * time.Minute),
			End:   anActivity.CreatedTs.Add(time.Duration(10) * time.Minute),
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 5, totalCount)

	// Can filter by GroupID, ChannelID, AppID, and InstanceID.
	totalCount, err = runtimeSvc(a).GetActivityCount(tTeam.ID,
		api.ActivityQueryParams{
			GroupID: tGroup.ID,
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 3, totalCount)
}
