package api_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/flatcar/nebraska/backend/pkg/api"
)

const (
	defaultTeamID = "d89342dc-9214-441d-a4af-bdd837a3b239"
)

func TestGetUser(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	_, err := runtimeSvc(a).GetUser("non-existent")
	assert.Error(t, err)

	user, err := runtimeSvc(a).GetUser("admin")
	assert.NoError(t, err)
	assert.Equal(t, "admin", user.Username)
	assert.Equal(t, defaultTeamID, user.TeamID)
	assert.Equal(t, "8b31292d4778582c0e5fa96aee5513f1", user.Secret)
}

func TestUpdateUserPassword(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	err := adminSvc(a).UpdateUserPassword("non-existent", "new-password")
	assert.Error(t, err)

	err = adminSvc(a).UpdateUserPassword("admin", "new-password")
	assert.NoError(t, err)

	user, err := runtimeSvc(a).GetUser("admin")
	assert.NoError(t, err)
	assert.Equal(t, "admin", user.Username)
	assert.Equal(t, defaultTeamID, user.TeamID)
	assert.NotEqual(t, "8b31292d4778582c0e5fa96aee5513f1", user.Secret)
	assert.Equal(t, "c01e8daa7a6c135909f218ff2bea1cfe", user.Secret)
}

func TestAddUser(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	user := &api.User{
		Username: "chandler",
		Secret:   "shhhhh",
		TeamID:   defaultTeamID,
	}

	chandler, err := adminSvc(a).AddUser(user)
	assert.NoError(t, err)
	assert.Equal(t, user.Username, chandler.Username)

	_, err = adminSvc(a).AddUser(user)
	assert.Error(t, err)
}

func TestGetUsersInTeam(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	_, err := runtimeSvc(a).GetUsersInTeam("non-existent")
	assert.Error(t, err)

	users, err := runtimeSvc(a).GetUsersInTeam(defaultTeamID)
	assert.NoError(t, err)
	assert.Equal(t, len(users), 1)

	teams, err := runtimeSvc(a).GetTeams()
	assert.NoError(t, err)
	assert.Equal(t, len(teams), 1)

	teamRoss, _ := adminSvc(a).AddTeam(&api.Team{Name: "team-ross"})
	assert.NoError(t, err)
	assert.Equal(t, teamRoss.Name, "team-ross")

	user := &api.User{
		Username: "chandler",
		Secret:   "shhhhh",
		TeamID:   teamRoss.ID,
	}

	chandler, err := adminSvc(a).AddUser(user)
	assert.NoError(t, err)
	assert.Equal(t, user.Username, chandler.Username)

	defaultUsers, err := runtimeSvc(a).GetUsersInTeam(defaultTeamID)
	assert.NoError(t, err)
	assert.Equal(t, len(defaultUsers), 1, "Should still be one.")

	newTeamUsers, err := runtimeSvc(a).GetUsersInTeam(teamRoss.ID)
	assert.NoError(t, err)
	assert.Equal(t, len(newTeamUsers), 1, "Should also be one.")
}
