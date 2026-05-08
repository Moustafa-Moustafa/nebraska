package api_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"gopkg.in/guregu/null.v4"

	"github.com/flatcar/nebraska/backend/pkg/api"
)

func TestAddApp(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})

	newApp, err := adminSvc(a).AddApp(&api.Application{Name: "app1", TeamID: tTeam.ID})
	assert.NoError(t, err)

	newAppX, err := runtimeSvc(a).GetApp(newApp.ID)
	assert.NoError(t, err)
	assert.Equal(t, "app1", newAppX.Name)

	_, err = adminSvc(a).AddApp(&api.Application{Name: "app1", TeamID: tTeam.ID})
	assert.Error(t, err, "App name must be unique per team.")

	_, err = adminSvc(a).AddApp(&api.Application{TeamID: tTeam.ID})
	assert.Error(t, err, "App name is required.")

	_, err = adminSvc(a).AddApp(&api.Application{Name: "app2"})
	assert.Error(t, err, "Team id is required.")

	_, err = adminSvc(a).AddApp(&api.Application{Name: "app2", TeamID: uuid.New().String()})
	assert.Error(t, err, "Team id used must exist.")

	_, err = adminSvc(a).AddApp(&api.Application{Name: "app2", TeamID: "invalidTeamID"})
	assert.Error(t, err, "Team id must be a valid uuid.")
}

func TestAddAppCloning(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	_, _ = adminSvc(a).AddGroup(&api.Group{Name: "group1", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	_, _ = adminSvc(a).AddGroup(&api.Group{Name: "group2", ApplicationID: tApp.ID, PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})

	clonedApp, err := adminSvc(a).AddAppCloning(&api.Application{Name: "app1", TeamID: tTeam.ID}, tApp.ID)
	assert.NoError(t, err)

	sourceApp, _ := runtimeSvc(a).GetApp(tApp.ID)
	clonedAppX, _ := runtimeSvc(a).GetApp(clonedApp.ID)
	assert.Equal(t, len(sourceApp.Groups), len(clonedAppX.Groups))
	assert.Equal(t, len(sourceApp.Channels), len(clonedAppX.Channels))

	// TODO: test specific fields in groups and channels (do not forget channel id in group!)

	_, err = adminSvc(a).AddAppCloning(&api.Application{Name: "app2", TeamID: tTeam.ID}, "")
	assert.NoError(t, err, "Using an empty source app id when cloning has the same effect as not cloning.")

	_, err = adminSvc(a).AddApp(&api.Application{Name: "productIDApp1", TeamID: tTeam.ID, ProductID: null.StringFrom("io.invalid. Name")})
	assert.Error(t, err)

	_, err = adminSvc(a).AddApp(&api.Application{Name: "productIDApp1", TeamID: tTeam.ID, ProductID: null.StringFrom("1io.invalid.Name")})
	assert.Error(t, err)

	_, err = adminSvc(a).AddApp(&api.Application{Name: "productIDApp1", TeamID: tTeam.ID, ProductID: null.StringFrom("")})
	assert.Error(t, err)

	_, err = adminSvc(a).AddApp(&api.Application{Name: "productIDApp1", TeamID: tTeam.ID, ProductID: null.StringFrom("io.invalid-.Name")})
	assert.Error(t, err)

	_, err = adminSvc(a).AddApp(&api.Application{Name: "productIDApp1", TeamID: tTeam.ID, ProductID: null.StringFrom("io.invalid_.Name")})
	assert.Error(t, err)

	_, err = adminSvc(a).AddApp(&api.Application{Name: "productIDApp1", TeamID: tTeam.ID, ProductID: null.StringFrom("io.valid.Name")})
	assert.NoError(t, err)

	_, err = adminSvc(a).AddApp(&api.Application{Name: "productIDApp2", TeamID: tTeam.ID, ProductID: null.StringFrom("io.valid.New-Name")})
	assert.NoError(t, err)

	_, err = adminSvc(a).AddApp(&api.Application{Name: "productIDApp3", TeamID: tTeam.ID, ProductID: null.StringFrom("io2.valid12.New-Name")})
	assert.NoError(t, err)

	_, err = adminSvc(a).AddApp(&api.Application{Name: "productIDApp4", TeamID: tTeam.ID, ProductID: null.StringFrom("io.invalid.New_Name")})
	assert.Error(t, err)

	tooLongName := `io.` +
		`loooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooong` +
		`loooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooong` +
		`loooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooong` +
		`loooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooong.example.com`

	_, err = adminSvc(a).AddApp(&api.Application{Name: "productIDApp5", TeamID: tTeam.ID, ProductID: null.StringFrom(tooLongName)})
	assert.Error(t, err)

	_, err = adminSvc(a).AddApp(
		&api.Application{
			Name:      "productIDApp4",
			TeamID:    tTeam.ID,
			ProductID: null.StringFrom("io.VALID.Name"),
		},
	)
	assert.Error(t, err, "duplicate name because it is case insensitive")
}

func TestUpdateApp(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", Description: "description", TeamID: tTeam.ID})

	err := adminSvc(a).UpdateApp(&api.Application{ID: tApp.ID, Name: "test_app_updated"})
	assert.NoError(t, err)

	app, _ := runtimeSvc(a).GetApp(tApp.ID)
	assert.Equal(t, "test_app_updated", app.Name)
	assert.Equal(t, "", app.Description, "Description set to empty string in last update as it wasn't provided")

	err = adminSvc(a).UpdateApp(&api.Application{ID: tApp.ID, Name: "test_app", Description: "description_updated"})
	assert.NoError(t, err)

	app, _ = runtimeSvc(a).GetApp(tApp.ID)
	assert.Equal(t, "test_app", app.Name)
	assert.Equal(t, "description_updated", app.Description)

	err = adminSvc(a).UpdateApp(&api.Application{Name: "test_app_updated_again"})
	assert.Error(t, err, "App id is required.")

	err = adminSvc(a).UpdateApp(&api.Application{ID: "invalidAppID", Name: "test_app_updated_again"})
	assert.Error(t, err, "App id must be a valid uuid.")
}

func TestDeleteApp(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})

	err := adminSvc(a).DeleteApp(tApp.ID)
	assert.NoError(t, err)

	_, err = runtimeSvc(a).GetApp(tApp.ID)
	assert.Error(t, err, "Trying to get deleted app.")
}

func TestGetApp(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, err := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	assert.NoError(t, err)
	tChannel, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID})
	tGroup, _ := adminSvc(a).AddGroup(&api.Group{Name: "group1", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	_, _ = runtimeSvc(a).RegisterInstance(uuid.New().String(), "", "10.0.0.1", "1.0.0", tApp.ID, tGroup.ID)

	app, err := runtimeSvc(a).GetApp(tApp.ID)
	assert.NoError(t, err)
	assert.Equal(t, tApp.Name, app.Name)
	assert.False(t, tApp.ProductID.Valid)
	assert.Equal(t, tChannel.Name, app.Channels[0].Name)
	assert.Equal(t, 1, app.Instances.Count)

	_, err = runtimeSvc(a).GetApp(uuid.New().String())
	assert.Error(t, err, "Trying to get non existent app.")

	tApp1, err := adminSvc(a).AddApp(&api.Application{Name: "test_app1", ProductID: null.StringFrom("io.flatcar.MyNewApp"), TeamID: tTeam.ID})
	assert.NoError(t, err)
	assert.NotEqual(t, null.StringFrom(""), tApp1.ProductID)

	app, err = runtimeSvc(a).GetApp(tApp1.ID)
	assert.NoError(t, err)
	assert.Equal(t, tApp1.Name, app.Name)

	appID, err := runtimeSvc(a).GetAppID(*tApp1.ProductID.Ptr())
	assert.NoError(t, err)

	app, err = runtimeSvc(a).GetApp(appID)
	assert.NoError(t, err)
	assert.Equal(t, tApp1.ProductID, app.ProductID)

	// App with same product_id
	_, err = adminSvc(a).AddApp(&api.Application{Name: "test_app2", ProductID: null.StringFrom("io.flatcar.MyNewApp"), TeamID: tTeam.ID})
	assert.Error(t, err)

	// App with a default product_id, to test the constraint is not limiting too much
	_, err = adminSvc(a).AddApp(&api.Application{Name: "test_app3", TeamID: tTeam.ID})
	assert.NoError(t, err)
}

func TestGetApps(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp1, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app1", TeamID: tTeam.ID})
	tApp2, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app2", TeamID: tTeam.ID})
	tChannel, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp1.ID})

	apps, err := runtimeSvc(a).GetApps(tTeam.ID, 0, 0)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(apps))
	assert.Equal(t, tApp1.Name, apps[1].Name)
	assert.Equal(t, tApp2.Name, apps[0].Name)
	assert.Equal(t, tChannel.Name, apps[1].Channels[0].Name)

	_, err = runtimeSvc(a).GetApps(uuid.New().String(), 0, 0)
	assert.NoError(t, err, "should not have any error for non existing appID")
}

func TestGetAppIDs(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp1, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app1", TeamID: tTeam.ID})
	tApp2, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app2", TeamID: tTeam.ID, ProductID: null.StringFrom("io.flatcar.MyApp2")})

	apps, err := runtimeSvc(a).GetApps(tTeam.ID, 0, 0)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(apps))
	assert.Equal(t, tApp1.Name, apps[1].Name)
	assert.Equal(t, tApp2.Name, apps[0].Name)

	app1ID, err := runtimeSvc(a).GetAppID(tApp1.ID)
	assert.NoError(t, err)
	assert.Equal(t, tApp1.ID, app1ID)

	app2ID, err := runtimeSvc(a).GetAppID(tApp2.ID)
	assert.NoError(t, err)
	assert.Equal(t, tApp2.ID, app2ID)

	_, err = runtimeSvc(a).GetAppID("io.flatcar.InvalidApp")
	assert.Error(t, err)

	_, err = runtimeSvc(a).GetAppID("")
	assert.Error(t, err)

	_, err = runtimeSvc(a).GetAppID("{")
	assert.Error(t, err)

	app2ID, err = runtimeSvc(a).GetAppID("io.flatcar.MyApp2")
	assert.NoError(t, err)
	assert.Equal(t, tApp2.ID, app2ID)

	tApp3, err := adminSvc(a).AddApp(&api.Application{Name: "test_app3", TeamID: tTeam.ID, ProductID: null.StringFrom("io.flatcar.MyApp3")})
	assert.NoError(t, err)

	app3ID, err := runtimeSvc(a).GetAppID("io.flatcar.MyApp3")
	assert.NoError(t, err)
	assert.Equal(t, tApp3.ID, app3ID)

	tApp2.ProductID = null.StringFrom("io.flatcar.App")
	err = adminSvc(a).UpdateApp(tApp2)
	assert.NoError(t, err)

	_, err = runtimeSvc(a).GetAppID("io.flatcar.MyApp2")
	assert.Error(t, err)
	assert.Equal(t, tApp2.ID, app2ID)

	app2ID, err = runtimeSvc(a).GetAppID("io.flatcar.App")
	assert.NoError(t, err)
	assert.Equal(t, tApp2.ID, app2ID)

	wrappedInBrackets := "{io.flatcar.App}"
	app2ID, err = runtimeSvc(a).GetAppID(wrappedInBrackets)
	assert.NoError(t, err)
	assert.Equal(t, tApp2.ID, app2ID)

	caseInsensitive := "io.Flatcar.app"
	app2ID, err = runtimeSvc(a).GetAppID(caseInsensitive)
	assert.NoError(t, err)
	assert.Equal(t, tApp2.ID, app2ID)
}

func TestGetAppsFiltered(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	tChannel, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	tGroup, _ := adminSvc(a).AddGroup(&api.Group{Name: "group1", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true, PolicySafeMode: true, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	realInstanceID := uuid.New().String()
	fakeInstanceID := "{" + uuid.New().String() + "}"
	_, _ = runtimeSvc(a).RegisterInstance(realInstanceID, "", "10.0.0.1", "1.0.0", tApp.ID, tGroup.ID)
	_, _ = runtimeSvc(a).RegisterInstance(fakeInstanceID, "", "10.0.0.1", "1.0.0", tApp.ID, tGroup.ID)

	// should ignore fake instance in Instances count
	apps, err := runtimeSvc(a).GetApps(tTeam.ID, 1, 10)
	assert.NoError(t, err)
	if assert.Len(t, apps, 1) {
		assert.Equal(t, 1, apps[0].Instances.Count)
	}
}
