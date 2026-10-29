package api

import (
	"fmt"
	"testing"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/guregu/null.v4"

	"github.com/flatcar/nebraska/backend/pkg/api/runtime"
	"github.com/flatcar/nebraska/backend/pkg/api/types"
)

type rolloutRow struct {
	name       string
	fake       bool
	version    string
	lastUpdate null.String
	grantedAgo time.Duration // 0 = never granted
	inProgress bool
	checkedAgo time.Duration
}

// fullGroupStats is what the rollout policy and the event handler read before
// the bounded rollout queries: counts over every active instance of the group.
// The bounded queries are checked against it.
type fullGroupStats struct {
	TotalInstances                   int `db:"total_instances"`
	UpdatesToCurrentVersionAttempted int `db:"updates_to_current_version_attempted"`
	UpdatesToCurrentVersionSucceeded int `db:"updates_to_current_version_succeeded"`
	UpdatesGrantedInLastPeriod       int `db:"updates_granted_in_last_period"`
	UpdatesInProgress                int `db:"updates_in_progress"`
	UpdatesTimedOut                  int `db:"updates_timed_out"`
}

func getFullGroupStats(t *testing.T, a *API, group *types.Group) fullGroupStats {
	t.Helper()
	packageVersion := ""
	if group.Channel != nil && group.Channel.Package != nil {
		packageVersion = group.Channel.Package.Version
	}
	query, _, err := goqu.From("instance_application").Select(
		goqu.COUNT("*").As("total_instances"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when update_in_progress = 'false' and last_update_version = ? then 1 else 0 end", packageVersion)), 0).As("updates_to_current_version_attempted"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when update_in_progress = 'false' and last_update_version = ? and last_update_version = version then 1 else 0 end", packageVersion)), 0).As("updates_to_current_version_succeeded"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when last_update_granted_ts > now() at time zone 'utc' - interval ? then 1 else 0 end", group.PolicyPeriodInterval)), 0).As("updates_granted_in_last_period"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when update_in_progress = 'true' and now() at time zone 'utc' - last_update_granted_ts <= interval ? then 1 else 0 end", group.PolicyUpdateTimeout)), 0).As("updates_in_progress"),
		goqu.COALESCE(goqu.SUM(goqu.L("case when update_in_progress = 'true' and now() at time zone 'utc' - last_update_granted_ts > interval ? then 1 else 0 end", group.PolicyUpdateTimeout)), 0).As("updates_timed_out"),
	).Where(goqu.C("group_id").Eq(group.ID), goqu.L("last_check_for_updates > now() at time zone 'utc' - interval '1 days'"),
		goqu.L("(instance_id IS NULL OR instance_id NOT LIKE '{________-____-____-____-____________}')"),
	).ToSQL()
	require.NoError(t, err)
	var stats fullGroupStats
	require.NoError(t, a.db().QueryRowx(query).StructScan(&stats))
	return stats
}

func TestRolloutQueriesAgreeWithFullCounts(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)

	tTeam, err := as.AddTeam(&types.Team{Name: "test_team"})
	require.NoError(t, err)
	tApp, err := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
	require.NoError(t, err)
	tPkg, err := as.AddPackage(&types.Package{Type: types.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	require.NoError(t, err)
	tChannel, err := as.AddChannel(&types.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
	require.NoError(t, err)
	policy := func(name string, channelID null.String) *types.Group {
		g, err := as.AddGroup(&types.Group{Name: name, ApplicationID: tApp.ID, ChannelID: channelID, PolicyUpdatesEnabled: true,
			PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 3, PolicyUpdateTimeout: "60 minutes"})
		require.NoError(t, err)
		g, err = a.GetGroup(g.ID)
		require.NoError(t, err)
		return g
	}
	withPkg := policy("with_package", null.StringFrom(tChannel.ID))
	noPkg := policy("no_package", null.String{})

	target, old := null.StringFrom("12.1.0"), null.StringFrom("11.0.0")
	minute := time.Minute
	// Succeeded rows first, so the first prefixes are complete rollouts.
	rows := []rolloutRow{
		{name: "succeeded", version: "12.1.0", lastUpdate: target, grantedAgo: 30 * minute, checkedAgo: minute},
		{name: "succeeded in period", version: "12.1.0", lastUpdate: target, grantedAgo: 5 * minute, checkedAgo: minute},
		{name: "stale in progress", version: "12.0.0", lastUpdate: target, grantedAgo: 5 * minute, inProgress: true, checkedAgo: 48 * time.Hour},
		{name: "fake in progress", fake: true, version: "12.0.0", lastUpdate: target, grantedAgo: 5 * minute, inProgress: true, checkedAgo: minute},
		{name: "in progress", version: "12.0.0", lastUpdate: target, grantedAgo: minute, inProgress: true, checkedAgo: minute},
		{name: "in progress on target version", version: "12.1.0", lastUpdate: target, grantedAgo: minute, inProgress: true, checkedAgo: minute},
		{name: "timed out", version: "12.0.0", lastUpdate: target, grantedAgo: 2 * time.Hour, inProgress: true, checkedAgo: minute},
		{name: "failed", version: "12.0.0", lastUpdate: target, grantedAgo: 20 * minute, checkedAgo: minute},
		{name: "never granted", version: "12.0.0", checkedAgo: minute},
		{name: "on target without grant", version: "12.1.0", checkedAgo: minute},
		{name: "older grant", version: "11.0.0", lastUpdate: old, grantedAgo: 10 * minute, checkedAgo: minute},
		{name: "newer version", version: "12.2.0", lastUpdate: target, grantedAgo: 10 * minute, checkedAgo: minute},
		{name: "on target with older grant", version: "12.1.0", lastUpdate: old, grantedAgo: 10 * minute, checkedAgo: minute},
		{name: "on target with newer grant", version: "12.1.0", lastUpdate: null.StringFrom("12.2.0"), grantedAgo: 10 * minute, checkedAgo: minute},
		{name: "in progress 2", version: "12.0.0", lastUpdate: target, grantedAgo: 2 * minute, inProgress: true, checkedAgo: minute},
		{name: "in progress 3", version: "12.0.0", lastUpdate: target, grantedAgo: 3 * minute, inProgress: true, checkedAgo: minute},
		{name: "succeeded 2", version: "12.1.0", lastUpdate: target, grantedAgo: 4 * minute, checkedAgo: minute},
	}

	check := func(t *testing.T, group *types.Group, step string) {
		want := getFullGroupStats(t, a, group)
		// The local override leaves the limit unbounded, and safe mode compares
		// the counts with 1 instead of the limit.
		for _, limit := range []int{-1, 0, 1, 2, 3, 100} {
			for _, withAttempted := range []bool{false, true} {
				got, err := a.GetGroupRolloutPolicyStats(group, limit, withAttempted)
				require.NoError(t, err)
				msg := fmt.Sprintf("%s, limit %d, with attempted %t", step, limit, withAttempted)
				limitOrOne := max(limit, 1)
				assert.Equal(t, min(want.UpdatesGrantedInLastPeriod, limitOrOne), got.UpdatesGrantedInLastPeriod, msg)
				assert.Equal(t, min(want.UpdatesInProgress, limitOrOne), got.UpdatesInProgress, msg)
				assert.Equal(t, min(want.UpdatesTimedOut, limitOrOne), got.UpdatesTimedOut, msg)
				for _, threshold := range []int{limit, 1} {
					assert.Equal(t, want.UpdatesGrantedInLastPeriod >= threshold, got.UpdatesGrantedInLastPeriod >= threshold, msg)
					assert.Equal(t, want.UpdatesInProgress >= threshold, got.UpdatesInProgress >= threshold, msg)
					assert.Equal(t, want.UpdatesTimedOut >= threshold, got.UpdatesTimedOut >= threshold, msg)
				}
				wantAttempted := 0
				if withAttempted {
					wantAttempted = min(want.UpdatesToCurrentVersionAttempted, 2)
				}
				assert.Equal(t, wantAttempted, got.UpdatesToCurrentVersionAttempted, msg)
			}
		}
		attempted, err := a.CountGroupUpdatesToCurrentVersionAttempted(group)
		require.NoError(t, err)
		assert.Equal(t, min(want.UpdatesToCurrentVersionAttempted, 2), attempted, step)
		complete, err := a.IsGroupRolloutComplete(group)
		require.NoError(t, err)
		assert.Equal(t, want.UpdatesToCurrentVersionSucceeded == want.TotalInstances, complete, step)
	}

	insert := func(group *types.Group, r rolloutRow) string {
		id := uuid.NewString()
		if r.fake {
			id = "{" + id + "}"
		}
		_, err := a.db().Exec("insert into instance (id, ip) values ($1, '10.0.0.1')", id)
		require.NoError(t, err)
		var granted null.Time
		if r.grantedAgo > 0 {
			granted = null.TimeFrom(time.Now().Add(-r.grantedAgo))
		}
		_, err = a.db().Exec(`insert into instance_application (instance_id, application_id, group_id, version,
			last_check_for_updates, last_update_granted_ts, last_update_version, update_in_progress)
			values ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, tApp.ID, group.ID, r.version, time.Now().Add(-r.checkedAgo), granted, r.lastUpdate, r.inProgress)
		require.NoError(t, err)
		return id
	}

	for _, group := range []*types.Group{withPkg, noPkg} {
		check(t, group, group.Name+": empty")
		// Each row alone, then alone next to two succeeded rows, then all rows together.
		isolated := func(prefix string) {
			for _, r := range rows[2:] {
				id := insert(group, r)
				check(t, group, group.Name+": "+prefix+r.name)
				_, err := a.db().Exec("delete from instance_application where instance_id = $1", id)
				require.NoError(t, err)
			}
		}
		isolated("only ")
		insert(group, rows[0])
		insert(group, rows[1])
		check(t, group, group.Name+": succeeded only")
		isolated("succeeded and ")
		for _, r := range rows[2:] {
			insert(group, r)
			check(t, group, group.Name+": up to "+r.name)
		}
	}
}

// The node-local override of max updates per period has no lower bound, so the
// rollout policy must treat 0 and negative values the way it always has, with
// and without safe mode.
func TestGetUpdatePackage_LocalMaxUpdatesOverrideBelowOne(t *testing.T) {
	for _, tc := range []struct {
		override      int
		safeMode      bool
		first, second error
		askSecond     bool
	}{
		{override: 0, safeMode: false, first: types.ErrMaxUpdatesPerPeriodLimitReached},
		{override: -1, safeMode: false, first: types.ErrMaxUpdatesPerPeriodLimitReached},
		// Safe mode lowers the limit to 1 until an update has been attempted,
		// so one instance gets through and the next one is held.
		{override: 0, safeMode: true, first: nil, second: types.ErrMaxUpdatesPerPeriodLimitReached, askSecond: true},
		{override: -1, safeMode: true, first: nil, second: types.ErrMaxUpdatesPerPeriodLimitReached, askSecond: true},
	} {
		t.Run(fmt.Sprintf("override %d, safe mode %t", tc.override, tc.safeMode), func(t *testing.T) {
			a := newForTest(t)
			defer a.Close()
			as := adminSvc(a)
			rs := runtimeSvc(a)

			tTeam, err := as.AddTeam(&types.Team{Name: "test_team"})
			require.NoError(t, err)
			tApp, err := as.AddApp(&types.Application{Name: "test_app", TeamID: tTeam.ID})
			require.NoError(t, err)
			tPkg, err := as.AddPackage(&types.Package{Type: types.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
			require.NoError(t, err)
			tChannel, err := as.AddChannel(&types.Channel{Name: "test_channel", Color: "blue", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})
			require.NoError(t, err)
			tGroup, err := as.AddGroup(&types.Group{Name: "group", ApplicationID: tApp.ID, ChannelID: null.StringFrom(tChannel.ID), PolicyUpdatesEnabled: true,
				PolicySafeMode: tc.safeMode, PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"})
			require.NoError(t, err)
			_, err = a.db().Exec("update group_local set policy_max_updates_per_period_override = $1 where group_id = $2", tc.override, tGroup.ID)
			require.NoError(t, err)

			_, err = rs.GetUpdatePackage(types.Instance{ID: uuid.New().String(), IP: "10.0.0.1"}, runtime.NewInstanceApplication(tApp.ID, tGroup.ID, "12.0.0"))
			assert.Equal(t, tc.first, err, "first instance")
			if tc.askSecond {
				_, err = rs.GetUpdatePackage(types.Instance{ID: uuid.New().String(), IP: "10.0.0.2"}, runtime.NewInstanceApplication(tApp.ID, tGroup.ID, "12.0.0"))
				assert.Equal(t, tc.second, err, "second instance, while the first one is updating")
			}
		})
	}
}
