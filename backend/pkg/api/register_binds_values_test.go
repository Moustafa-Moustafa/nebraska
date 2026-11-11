package api

// Review only, not part of the PR: it shows that the change keeps main's
// behaviour, and it is not meant to be merged.

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flatcar/nebraska/backend/pkg/api/runtime"
	"github.com/flatcar/nebraska/backend/pkg/api/types"
)

func TestRegisterInstanceBindsInstanceValues(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	// One connection, so the registrations and the check share a session.
	a.db().SetMaxOpenConns(1)
	as := adminSvc(a)
	rs := runtimeSvc(a)

	tTeam, err := as.AddTeam(&types.Team{Name: "test_team"})
	require.NoError(t, err)
	tApp, err := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
	require.NoError(t, err)
	group, err := as.AddGroup(&types.Group{Name: "group", ApplicationID: tApp.ID, PolicyUpdatesEnabled: true,
		PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 1000, PolicyUpdateTimeout: "60 minutes"})
	require.NoError(t, err)

	for i := 0; i < 20; i++ {
		inst := types.Instance{ID: uuid.NewString(), IP: fmt.Sprintf("10.0.0.%d", i)}
		instApp := runtime.NewInstanceApplication(tApp.ID, group.ID, fmt.Sprintf("12.%d.0", i))
		_, err := rs.RegisterInstance(inst, instApp)
		require.NoError(t, err)
		// Re-register with a new IP: the single-table write path.
		inst.IP = "10.1.0.1"
		_, err = rs.RegisterInstance(inst, instApp)
		require.NoError(t, err)
	}

	count := func(like string) int {
		var n int
		require.NoError(t, a.db().QueryRow(`select count(*) from pg_prepared_statements where statement not like '%pg_prepared_statements%' and statement like $1`, like).Scan(&n))
		return n
	}
	assert.Equal(t, 1, count(`SELECT i.*, (SELECT to_jsonb(ia)%`))
	assert.Equal(t, 1, count(`INSERT INTO "instance" %`))
	assert.Equal(t, 1, count(`INSERT INTO "instance_application" %`))
}
