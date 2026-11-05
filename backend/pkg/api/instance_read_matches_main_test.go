package api

// Review only, not part of the PR: it shows that the change keeps main's
// behaviour, and it is not meant to be merged.

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/guregu/null.v4"

	"github.com/flatcar/nebraska/backend/pkg/api/types"
)

// GetInstance reads an instance with its application row in one statement. The
// two statements it used to run are the reference.
func TestGetInstanceMatchesTwoStatementRead(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)

	tTeam, err := as.AddTeam(&types.Team{Name: "test_team"})
	require.NoError(t, err)
	tApp, err := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
	require.NoError(t, err)
	tApp2, err := as.AddApp(&types.Application{Name: "test_app2", TeamID: tTeam.ID})
	require.NoError(t, err)
	group, err := as.AddGroup(&types.Group{Name: "group", ApplicationID: tApp.ID, PolicyUpdatesEnabled: true,
		PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
	require.NoError(t, err)

	type appRow struct {
		app        string
		groupID    null.String
		status     null.Int
		lastUpdate null.String
		inProgress bool
		checkedAgo time.Duration
	}
	cases := map[string]*appRow{
		"no application row":         nil,
		"recent, all values":         {app: tApp.ID, groupID: null.StringFrom(group.ID), status: null.IntFrom(2), lastUpdate: null.StringFrom("2.0.0"), inProgress: true, checkedAgo: time.Minute},
		"recent, nulls":              {app: tApp.ID, checkedAgo: time.Minute},
		"checked 2 days ago":         {app: tApp.ID, groupID: null.StringFrom(group.ID), status: null.IntFrom(4), checkedAgo: 48 * time.Hour},
		"row for another app only":   {app: tApp2.ID, checkedAgo: time.Minute},
		"recent, checked 23h59m ago": {app: tApp.ID, status: null.IntFrom(4), checkedAgo: 24*time.Hour - time.Minute},
	}
	readInTwoStatements := func(id, appID string) (*types.Instance, error) {
		var instance types.Instance
		if err := a.db().QueryRowx(`SELECT * FROM instance WHERE id = $1`, id).StructScan(&instance); err != nil {
			return nil, err
		}
		err := a.db().QueryRowx(`SELECT version, status, last_check_for_updates, last_update_version, update_in_progress,
			application_id, group_id, instance_id FROM instance_application WHERE application_id = $1 AND instance_id = $2
			AND last_check_for_updates > now() at time zone 'utc' - interval '1 days'`, appID, id).StructScan(&instance.Application)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
		return &instance, err
	}
	for name, c := range cases {
		id := uuid.NewString()
		_, err := a.db().Exec("insert into instance (id, ip, alias, oem, aleph_version) values ($1, '10.0.0.1', 'alias', 'azure', '1.0.0')", id)
		require.NoError(t, err)
		if c != nil {
			_, err = a.db().Exec(`insert into instance_application (instance_id, application_id, group_id, version, status,
				last_check_for_updates, last_update_version, update_in_progress) values ($1, $2, $3, '1.0.0', $4, $5, $6, $7)`,
				id, c.app, c.groupID, c.status, time.Now().Add(-c.checkedAgo), c.lastUpdate, c.inProgress)
			require.NoError(t, err)
		}
		want, err := readInTwoStatements(id, tApp.ID)
		require.NoError(t, err, name)
		got, err := a.GetInstance(id, tApp.ID)
		require.NoError(t, err, name)
		assert.Equal(t, want, got, name)
	}

	_, err = a.GetInstance(uuid.NewString(), tApp.ID)
	require.ErrorIs(t, err, sql.ErrNoRows)
}
