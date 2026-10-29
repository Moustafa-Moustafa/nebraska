package api

import (
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/guregu/null.v4"

	"github.com/flatcar/nebraska/backend/pkg/api/runtime"
	"github.com/flatcar/nebraska/backend/pkg/api/types"
)

func TestAddGroup(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)

	tTeam, _ := as.AddTeam(&types.Team{Name: "test_team"})
	tApp, _ := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
	tApp2, _ := as.AddApp(&types.Application{Name: "test_app2", TeamID: tTeam.ID})
	tPkg, _ := as.AddPackage(&types.Package{Type: types.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tPkg2, _ := as.AddPackage(&types.Package{Type: types.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp2.ID})
	tChannel, _ := as.AddChannel(&types.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tChannel2, _ := as.AddChannel(&types.Channel{Name: "test_channel2", Color: "yellow", ApplicationID: tApp2.ID, PackageID: null.StringFrom(tPkg2.ID)})

	group := &types.Group{
		Name:                      "group1",
		Description:               "description",
		ApplicationID:             tApp.ID,
		ChannelID:                 null.StringFrom(tChannel.ID),
		PolicyUpdatesEnabled:      true,
		PolicySafeMode:            true,
		PolicyPeriodInterval:      "15 minutes",
		PolicyMaxUpdatesPerPeriod: 2,
		PolicyUpdateTimeout:       "60 minutes",
	}
	group, err := as.AddGroup(group)
	assert.NoError(t, err)
	assert.Equal(t, true, group.PolicyUpdatesEnabled)

	_, err = a.GetGroupRolloutPolicyStats(group, group.PolicyMaxUpdatesPerPeriod, group.PolicySafeMode)
	assert.NoError(t, err)

	groupX, err := a.GetGroup(group.ID)
	assert.NoError(t, err)
	assert.Equal(t, group.Name, groupX.Name)
	assert.Equal(t, group.Description, groupX.Description)
	assert.Equal(t, group.PolicyUpdatesEnabled, groupX.PolicyUpdatesEnabled)
	assert.Equal(t, group.PolicySafeMode, groupX.PolicySafeMode)
	assert.Equal(t, group.PolicyPeriodInterval, groupX.PolicyPeriodInterval)
	assert.Equal(t, group.PolicyMaxUpdatesPerPeriod, groupX.PolicyMaxUpdatesPerPeriod)
	assert.Equal(t, group.PolicyUpdateTimeout, groupX.PolicyUpdateTimeout)
	assert.Equal(t, tApp.ID, groupX.ApplicationID)
	assert.Equal(t, null.StringFrom(tChannel.ID), groupX.ChannelID)
	assert.Equal(t, tChannel.Name, groupX.Channel.Name)
	assert.Equal(t, tPkg.Version, groupX.Channel.Package.Version)

	_, err = as.AddGroup(&types.Group{Name: "test_group", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel2.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	assert.Equal(t, types.ErrInvalidChannel, err, "Channel id used doesn't belong to the application id that this group will be bound to and it should.")
}

func TestUpdateGroup(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)

	tTeam, _ := as.AddTeam(&types.Team{Name: "test_team"})
	tApp1, _ := as.AddApp(&types.Application{Name: "test_app1", TeamID: tTeam.ID})
	tApp2, _ := as.AddApp(&types.Application{Name: "test_app2", TeamID: tTeam.ID})
	tPkg, _ := as.AddPackage(&types.Package{Type: types.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp1.ID})
	tChannel1, _ := as.AddChannel(&types.Channel{Name: "test_channel1", Color: "blue", ApplicationID: tApp1.ID, PackageID: null.StringFrom(tPkg.ID)})
	tChannel2, _ := as.AddChannel(&types.Channel{Name: "test_channel2", Color: "green", ApplicationID: tApp1.ID})
	tChannel3, _ := as.AddChannel(&types.Channel{Name: "test_channel3", Color: "red", ApplicationID: tApp2.ID})

	group, _ := as.AddGroup(&types.Group{Name: "group1", ApplicationID: tApp1.ID, ChannelID: null.StringFrom(tChannel1.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	group.Name = "group1_updated"
	group.PolicyUpdatesEnabled = true
	group.ChannelID = null.StringFrom(tChannel2.ID)
	err := as.UpdateGroup(group)
	assert.NoError(t, err)

	_, err = a.GetGroupRolloutPolicyStats(group, group.PolicyMaxUpdatesPerPeriod, group.PolicySafeMode)
	assert.NoError(t, err)

	groupX, _ := a.GetGroup(group.ID)
	assert.Equal(t, group.Name, groupX.Name)
	assert.Equal(t, group.PolicyPeriodInterval, groupX.PolicyPeriodInterval)
	assert.Equal(t, group.PolicyUpdatesEnabled, groupX.PolicyUpdatesEnabled)
	assert.Equal(t, tChannel2.Name, groupX.Channel.Name)

	groupX.ApplicationID = tApp2.ID
	err = as.UpdateGroup(groupX)
	assert.NoError(t, err, "Application id cannot be updated, but it won't produce an error.")

	groupX, _ = a.GetGroup(group.ID)
	assert.Equal(t, tApp1.ID, groupX.ApplicationID)

	groupX.ChannelID = null.StringFrom(tChannel3.ID)
	err = as.UpdateGroup(groupX)
	assert.Equal(t, types.ErrInvalidChannel, err, "Channel id used doesn't belong to the application id that this group is bound to and it should.")
}

func TestDeleteGroup(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)

	tTeam, _ := as.AddTeam(&types.Team{Name: "test_team"})
	tApp, _ := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := as.AddPackage(&types.Package{Type: types.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := as.AddChannel(&types.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup, _ := as.AddGroup(&types.Group{Name: "test_group", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})

	err := as.DeleteGroup(tGroup.ID)
	assert.NoError(t, err)

	_, err = a.GetGroup(tGroup.ID)
	assert.Error(t, err, "Trying to get deleted group.")
}

func TestGetGroup(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)

	tTeam, _ := as.AddTeam(&types.Team{Name: "test_team"})
	tApp, _ := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := as.AddPackage(&types.Package{Type: types.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := as.AddChannel(&types.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup, _ := as.AddGroup(&types.Group{Name: "test_group", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})

	group, err := a.GetGroup(tGroup.ID)
	assert.NoError(t, err)
	assert.Equal(t, tGroup.Name, group.Name)
	assert.Equal(t, tApp.ID, group.ApplicationID)
	assert.Equal(t, tChannel.Name, group.Channel.Name)
	assert.Equal(t, tPkg.Version, group.Channel.Package.Version)

	_, err = a.GetGroup(uuid.New().String())
	assert.Error(t, err, "Trying to get non existent group.")
}

func TestGetGroupSeparatesOverrideFromAdminPolicy(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)

	tTeam, _ := as.AddTeam(&types.Team{Name: "test_team"})
	tApp, _ := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
	tGroup, err := as.AddGroup(&types.Group{Name: "test_group", ApplicationID: tApp.ID, PolicyUpdatesEnabled: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	require.NoError(t, err)

	group, err := a.GetGroup(tGroup.ID)
	require.NoError(t, err)
	assert.True(t, group.AdminPolicy.PolicyUpdatesEnabled)
	assert.False(t, group.LocalOverrides.PolicyUpdatesEnabled.Valid)

	_, err = a.db().Exec(`update group_local set policy_updates_enabled_override = false, policy_safe_mode_override = true,
		policy_office_hours_override = true, policy_timezone_override = 'Europe/Berlin', policy_period_interval_override = '1 hours',
		policy_max_updates_per_period_override = 9, policy_update_timeout_override = '9 minutes' where group_id = $1`, tGroup.ID)
	require.NoError(t, err)

	group, err = a.GetGroup(tGroup.ID)
	require.NoError(t, err)
	// The rollout engine reads the top-level fields, so they must keep the brake.
	assert.False(t, group.PolicyUpdatesEnabled)
	assert.Equal(t, 9, group.PolicyMaxUpdatesPerPeriod)
	assert.True(t, group.AdminPolicy.PolicyUpdatesEnabled)
	assert.Equal(t, null.BoolFrom(false), group.LocalOverrides.PolicyUpdatesEnabled)
	assert.Equal(t, null.IntFrom(9), group.LocalOverrides.PolicyMaxUpdatesPerPeriod)

	group.ShowAdminPolicy()
	assert.Equal(t, types.GroupPolicy{
		PolicyUpdatesEnabled:      true,
		PolicyPeriodInterval:      "15 minutes",
		PolicyMaxUpdatesPerPeriod: 2,
		PolicyUpdateTimeout:       "60 minutes",
	}, types.GroupPolicy{
		PolicyUpdatesEnabled:      group.PolicyUpdatesEnabled,
		PolicySafeMode:            group.PolicySafeMode,
		PolicyOfficeHours:         group.PolicyOfficeHours,
		PolicyTimezone:            group.PolicyTimezone,
		PolicyPeriodInterval:      group.PolicyPeriodInterval,
		PolicyMaxUpdatesPerPeriod: group.PolicyMaxUpdatesPerPeriod,
		PolicyUpdateTimeout:       group.PolicyUpdateTimeout,
	})
}

func TestGetGroups(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)

	tTeam, _ := as.AddTeam(&types.Team{Name: "test_team"})
	tApp, _ := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := as.AddPackage(&types.Package{Type: types.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := as.AddChannel(&types.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup1, _ := as.AddGroup(&types.Group{Name: "test_group1", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	tGroup2, _ := as.AddGroup(&types.Group{Name: "test_group2", ApplicationID: tApp.ID, PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})

	groups, err := a.GetGroups(tApp.ID, 0, 0)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(groups))
	assert.Equal(t, tGroup2.Name, groups[0].Name)
	assert.Equal(t, tGroup1.Name, groups[1].Name)
	assert.Equal(t, tChannel.Name, groups[1].Channel.Name)
	assert.Equal(t, tPkg.ID, groups[1].Channel.PackageID.String)
	assert.Equal(t, tPkg.Version, groups[1].Channel.Package.Version)
}

func TestGetGroupsFiltered(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)
	rs := runtimeSvc(a)

	tTeam, _ := as.AddTeam(&types.Team{Name: "test_team"})
	tApp, _ := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := as.AddPackage(&types.Package{Type: types.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := as.AddChannel(&types.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup, _ := as.AddGroup(&types.Group{Name: "test_group1", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	realInstanceID := uuid.New().String()
	fakeInstanceID1 := "{" + uuid.New().String() + "}"
	fakeInstanceID2 := "{" + uuid.New().String() + "}"
	_, _ = rs.RegisterInstance(types.Instance{ID: realInstanceID, IP: "10.0.0.1"}, runtime.NewInstanceApplication(tApp.ID, tGroup.ID, "1.0.0"))
	_, _ = rs.RegisterInstance(types.Instance{ID: fakeInstanceID1, IP: "10.0.0.1"}, runtime.NewInstanceApplication(tApp.ID, tGroup.ID, "2.0.0"))
	_, _ = rs.RegisterInstance(types.Instance{ID: fakeInstanceID2, IP: "10.0.0.1"}, runtime.NewInstanceApplication(tApp.ID, tGroup.ID, "1.0.0"))

	groups, err := a.GetGroups(tApp.ID, 0, 0)
	assert.NoError(t, err)
	if assert.Len(t, groups, 1) {
		g := groups[0]
		stats, err := a.GetGroupInstancesStats(g.ID, testDuration)
		assert.NoError(t, err)
		assert.Equal(t, 1, stats.Total)
		versionBreakdown, vbErr := a.GetGroupVersionBreakdown(g.ID)
		assert.NoError(t, vbErr)
		if assert.Len(t, versionBreakdown, 1) {
			vb := versionBreakdown[0]
			assert.Equal(t, "1.0.0", vb.Version)
			assert.Equal(t, 1, vb.Instances)
		}
	}
}

func TestVersionBreakDownEmpty(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)

	tTeam, _ := as.AddTeam(&types.Team{Name: "test_team"})
	tApp, _ := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := as.AddPackage(&types.Package{Type: types.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := as.AddChannel(&types.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	_, err := as.AddGroup(&types.Group{Name: "test_group1", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	assert.NoError(t, err)

	groups, err := a.GetGroups(tApp.ID, 0, 0)
	assert.NoError(t, err)
	g := groups[0]

	versionBreakdown, vbErr := a.GetGroupVersionBreakdown(g.ID)
	assert.NoError(t, vbErr)
	assert.Len(t, versionBreakdown, 0)
}

func TestGroupTrackName(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)

	trackName := "production"
	tTeam, _ := as.AddTeam(&types.Team{Name: "test_team"})
	tApp, _ := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := as.AddPackage(&types.Package{Type: types.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := as.AddChannel(&types.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup1, _ := as.AddGroup(&types.Group{Name: "test_group", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes", Track: trackName})

	id, err := a.GetGroupID(tApp.ID, trackName, types.ArchAll)
	assert.NoError(t, err)
	assert.Equal(t, tGroup1.ID, id)

	_, err = a.GetGroupID(tApp.ID, "", types.ArchAll)
	assert.Error(t, err, "no group found for track  and architecture amd64")

	_, err = a.GetGroupID(tApp.ID, "Phony", types.ArchAll)
	assert.Error(t, err, "no group found for track Phony and architecture amd64")

	tGroup2, err := as.AddGroup(&types.Group{Name: "test_group2", ApplicationID: tApp.ID, PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	assert.NoError(t, err)
	assert.Equal(t, tGroup2.Track, tGroup2.ID)

	// Check adding two groups with the same track name, in different apps, and getting them
	tApp2, err := as.AddApp(&types.Application{Name: "test_app2", TeamID: tTeam.ID})
	assert.NoError(t, err)
	tPkgApp2, _ := as.AddPackage(&types.Package{Type: types.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp2.ID})
	tChannelApp2, _ := as.AddChannel(&types.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp2.ID, PackageID: null.StringFrom(tPkgApp2.ID)})
	tGroupApp2, err := as.AddGroup(&types.Group{Name: "beta", ApplicationID: tApp2.ID, ChannelID: null.StringFrom(tChannelApp2.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes", Track: "beta"})
	assert.NoError(t, err)

	tGroup3, err := as.AddGroup(&types.Group{Name: "beta", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes", Track: "beta"})
	assert.NoError(t, err)

	betaGroupID, err := a.GetGroupID(tApp2.ID, "beta", types.ArchAll)
	assert.NoError(t, err)
	assert.Equal(t, tGroupApp2.ID, betaGroupID)

	betaGroupID, err = a.GetGroupID(tApp.ID, "beta", types.ArchAll)
	assert.NoError(t, err)
	assert.Equal(t, tGroup3.ID, betaGroupID)

	// Test group with a track name but no arch (because there's no channel assigned to it)
	tGroupNoChannel, err := as.AddGroup(&types.Group{Name: "unknown", ApplicationID: tApp.ID, PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes", Track: "unknown"})
	assert.NoError(t, err)

	_, err = a.GetGroupID(tApp.ID, tGroupNoChannel.Track, types.ArchAll)
	assert.Error(t, err, "no group found")
}

// TestGetVersionCountTimelineInjection verifies groupID is bound as a parameter
// and a malicious value cannot leak instances.
func TestGetVersionCountTimelineInjection(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)
	rs := runtimeSvc(a)

	tTeam, _ := as.AddTeam(&types.Team{Name: "test_team"})
	tApp, _ := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := as.AddPackage(&types.Package{Type: types.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := as.AddChannel(&types.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup, _ := as.AddGroup(&types.Group{Name: "injection_group", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})

	instanceID := uuid.New().String()
	_, _ = rs.RegisterInstance(types.Instance{ID: instanceID, IP: "10.0.0.1"}, runtime.NewInstanceApplication(tApp.ID, tGroup.ID, "1.0.0"))

	total := func(m map[time.Time]types.VersionCountMap) (n uint64) {
		for _, vm := range m {
			for _, c := range vm {
				n += c
			}
		}
		return
	}

	// Baseline: the instance is visible for its real group, so a zero result
	// below cannot be mistaken for an empty fixture.
	legit, _, err := a.GetGroupVersionCountTimeline(tGroup.ID, "1h")
	assert.NoError(t, err)
	assert.NotZero(t, total(legit), "baseline must see the instance")

	leaked, _, _ := a.GetGroupVersionCountTimeline("00000000-0000-0000-0000-000000000000' OR '1'='1", "1h")
	assert.Zero(t, total(leaked), "malicious groupID must not leak instances")
}

// TestGetVersionCountTimelineRejectsSQLInjection asserts the statement never
// runs, not merely that it returns nothing. The payload writes if the groupID
// reaches the SQL text, so an unchanged canary is evidence of non execution.
func TestGetVersionCountTimelineRejectsSQLInjection(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	before, err := a.GetUser("admin")
	require.NoError(t, err)

	payload := `00000000-0000-0000-0000-000000000000') ish ON true), ` +
		`injected AS (UPDATE users SET secret = 'sql-injection-canary' ` +
		`WHERE username = 'admin' RETURNING 1) ` +
		`SELECT now() AS ts, ''::text AS version, 0::bigint AS total --`

	_, _, err = a.GetGroupVersionCountTimeline(payload, "1h")
	require.Error(t, err)

	after, err := a.GetUser("admin")
	require.NoError(t, err)
	require.Equal(t, before.Secret, after.Secret,
		"malicious groupID must not execute a database write")
}

func TestGetGroupInOneQueryMatchesGetGroup(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)

	tTeam, err := as.AddTeam(&types.Team{Name: "test_team"})
	require.NoError(t, err)
	tApp, err := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
	require.NoError(t, err)

	addPkg := func(version string, arch types.Arch, action *types.FlatcarAction, files []types.File, labels types.Labels) *types.Package {
		p, err := as.AddPackage(&types.Package{Type: types.PkgTypeFlatcar, URL: "http://sample.url/pkg", Version: version,
			ApplicationID: tApp.ID, Arch: arch, Filename: null.StringFrom("update.gz"), Size: null.StringFrom("123"),
			Hash: null.StringFrom("aGFzaA=="), FlatcarAction: action, ExtraFiles: files, Labels: labels})
		require.NoError(t, err)
		return p
	}
	addChannel := func(name string, arch types.Arch, pkg *types.Package) *types.Channel {
		c := &types.Channel{Name: name, Color: "blue", ApplicationID: tApp.ID, Arch: arch}
		if pkg != nil {
			c.PackageID = null.StringFrom(pkg.ID)
		}
		c, err := as.AddChannel(c)
		require.NoError(t, err)
		return c
	}
	groups := map[string]string{}
	addGroup := func(name string, channel *types.Channel) {
		g := &types.Group{Name: name, ApplicationID: tApp.ID, Track: name, PolicyUpdatesEnabled: true, PolicySafeMode: true,
			PolicyTimezone: null.StringFrom("Europe/Berlin"), PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"}
		if channel != nil {
			g.ChannelID = null.StringFrom(channel.ID)
		}
		g, err := as.AddGroup(g)
		require.NoError(t, err)
		groups[name] = g.ID
	}

	action := &types.FlatcarAction{Event: "postinstall", ChromeOSVersion: "", Sha256: "c2hh", NeedsAdmin: false, IsDelta: false,
		DisablePayloadBackoff: true, MetadataSignatureRsa: "", MetadataSize: "", Deadline: ""}
	files := []types.File{{Name: null.StringFrom("b.raw"), Size: null.StringFrom("2"), Hash: null.StringFrom("aA=="), Hash256: null.StringFrom("bB==")},
		{Name: null.StringFrom("a.raw"), Size: null.StringFrom("1")}}

	addGroup("no channel", nil)
	addGroup("channel without package", addChannel("bare", types.ArchAMD64, nil))
	addGroup("plain package", addChannel("plain", types.ArchAMD64, addPkg("1.0.0", types.ArchAMD64, nil, nil, nil)))
	full := addPkg("2.0.0", types.ArchAArch64, action, files, types.Labels{"tier": "gold", "zone": "a"})
	fullChannel := addChannel("full", types.ArchAArch64, full)
	addGroup("package with action, files and blacklist", fullChannel)
	other := addChannel("other", types.ArchAArch64, nil)
	_, err = a.db().Exec("insert into package_channel_blacklist (package_id, channel_id) values ($1, $2), ($1, $3)", full.ID, other.ID, fullChannel.ID)
	require.NoError(t, err)
	addGroup("local overrides", addChannel("local", types.ArchAMD64, addPkg("3.0.0", types.ArchAMD64, action, nil, nil)))
	_, err = a.db().Exec(`update group_local set rollout_in_progress = true, policy_safe_mode_override = false,
		policy_max_updates_per_period_override = 9, policy_timezone_override = 'UTC' where group_id = $1`, groups["local overrides"])
	require.NoError(t, err)

	for name, id := range groups {
		want, err := a.GetGroup(id)
		require.NoError(t, err, name)
		got, err := a.GetGroupInOneQuery(id)
		require.NoError(t, err, name)
		// The blacklist is an unordered aggregate in both queries.
		if want.Channel != nil && want.Channel.Package != nil {
			slices.Sort(want.Channel.Package.ChannelsBlacklist)
			slices.Sort(got.Channel.Package.ChannelsBlacklist)
		}
		assert.Equal(t, want, got, name)
	}
	g, err := a.GetGroupInOneQuery(groups["package with action, files and blacklist"])
	require.NoError(t, err)
	require.Len(t, g.Channel.Package.ExtraFiles, 2)
	require.Len(t, g.Channel.Package.ChannelsBlacklist, 2)
	require.NotNil(t, g.Channel.Package.FlatcarAction)
	require.Equal(t, types.Labels{"tier": "gold", "zone": "a"}, g.Channel.Package.Labels)

	_, err = a.GetGroup(uuid.NewString())
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = a.GetGroupInOneQuery(uuid.NewString())
	require.ErrorIs(t, err, sql.ErrNoRows)

	// Other spellings of the id behave as they did in GetGroup.
	id := groups["plain package"]
	for _, spelling := range []string{"{" + id + "}", strings.ToUpper(id), strings.ReplaceAll(id, "-", ""), "urn:uuid:" + id, "not-a-uuid", ""} {
		want, wantErr := a.GetGroup(spelling)
		got, gotErr := a.GetGroupInOneQuery(spelling)
		assert.Equal(t, wantErr == nil, gotErr == nil, spelling)
		assert.Equal(t, want, got, spelling)
	}

	// GetGroup fails when an action column it reads into a plain Go type is NULL.
	id = groups["package with action, files and blacklist"]
	for _, column := range []string{"event", "needs_admin"} {
		_, err = a.db().Exec("update flatcar_action set event = 'postinstall', needs_admin = false where package_id = $1", full.ID)
		require.NoError(t, err)
		_, err = a.db().Exec("update flatcar_action set "+column+" = NULL where package_id = $1", full.ID)
		require.NoError(t, err)
		_, err = a.GetGroup(id)
		require.Error(t, err, column)
		_, err = a.GetGroupInOneQuery(id)
		require.Error(t, err, column)
	}
}
