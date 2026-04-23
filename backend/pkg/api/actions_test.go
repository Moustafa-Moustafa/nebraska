package api_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/flatcar/nebraska/backend/pkg/api"
)

func TestAddFlatcarAction(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, _ := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeFlatcar, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})

	flatcarAction, err := adminSvc(a).AddFlatcarAction(&api.FlatcarAction{Event: "postinstall", Sha256: "fsdkjjfghsdakjfgaksdjfasd", PackageID: tPkg.ID})
	assert.NoError(t, err)

	flatcarActionX, err := runtimeSvc(a).GetFlatcarAction(tPkg.ID)
	assert.NoError(t, err)

	assert.Equal(t, flatcarAction.Event, flatcarActionX.Event)
	assert.Equal(t, flatcarAction.Sha256, flatcarActionX.Sha256)
}
