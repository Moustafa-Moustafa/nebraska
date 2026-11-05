package api

import (
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/guregu/null.v4"

	"github.com/flatcar/nebraska/backend/pkg/api/types"
)

// Review only, not part of the PR: the channel and group reads return the
// channel main's GetChannel returned. mainChannel rebuilds main's GetChannel from the channel
// row and GetPackage, which still reads the package the way main did.
func TestReviewChannelReadsMatchMain(t *testing.T) {
	a := newForTest(t)
	defer a.Close()
	as := adminSvc(a)

	tTeam, err := as.AddTeam(&types.Team{Name: "eq_team"})
	require.NoError(t, err)
	tApp, err := as.AddApp(&types.Application{Name: "eq_app", TeamID: tTeam.ID})
	require.NoError(t, err)

	addPkg := func(version string, arch types.Arch, action *types.FlatcarAction, files []types.File, labels types.Labels) *types.Package {
		pkg := &types.Package{Type: types.PkgTypeFlatcar, URL: "http://sample.url/pkg", Version: version,
			Description: null.StringFrom("package " + version), ApplicationID: tApp.ID, Arch: arch,
			Filename: null.StringFrom("update.gz"), Size: null.StringFrom("123"), Hash: null.StringFrom("aGFzaA=="),
			FlatcarAction: action, ExtraFiles: files, Labels: labels}
		add := as.AddPackage
		if action != nil {
			add = as.AddPackageWithMetadata
		}
		p, err := add(pkg)
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
	addGroup := func(name string, channel *types.Channel) string {
		g := &types.Group{Name: name, ApplicationID: tApp.ID, Track: name, PolicyUpdatesEnabled: true, PolicySafeMode: true,
			PolicyTimezone: null.StringFrom("Europe/Berlin"), PolicyPeriodInterval: "15 minutes", PolicyMaxUpdatesPerPeriod: 2, PolicyUpdateTimeout: "60 minutes"}
		if channel != nil {
			g.ChannelID = null.StringFrom(channel.ID)
		}
		g, err := as.AddGroup(g)
		require.NoError(t, err)
		return g.ID
	}

	action := &types.FlatcarAction{Event: "postinstall", ChromeOSVersion: "16.0.0", Sha256: "c2hh", NeedsAdmin: true, IsDelta: true,
		DisablePayloadBackoff: true, MetadataSignatureRsa: "c2ln", MetadataSize: "42", Deadline: "now"}
	files := []types.File{{Name: null.StringFrom("b.raw"), Size: null.StringFrom("2"), Hash: null.StringFrom("aA=="), Hash256: null.StringFrom("bB==")},
		{Name: null.StringFrom("a.raw"), Size: null.StringFrom("1")}}
	full := addPkg("2.0.0", types.ArchAArch64, action, files, types.Labels{"tier": "gold"})
	channels := []*types.Channel{
		addChannel("bare", types.ArchAMD64, nil),
		addChannel("plain", types.ArchAMD64, addPkg("1.0.0", types.ArchAMD64, nil, nil, nil)),
		addChannel("full", types.ArchAArch64, full),
		addChannel("action only", types.ArchAMD64, addPkg("3.0.0", types.ArchAMD64, action, nil, nil)),
	}
	_, err = a.db().Exec("insert into package_channel_blacklist (package_id, channel_id) values ($1, $2), ($1, $3)", full.ID, channels[0].ID, channels[2].ID)
	require.NoError(t, err)
	groupIDs := []string{addGroup("no channel", nil)}
	for _, c := range channels {
		groupIDs = append(groupIDs, addGroup("on "+c.Name, c))
	}

	mainChannel := func(id string) (*types.Channel, error) {
		var c types.Channel
		if err := a.db().QueryRowx("SELECT * FROM channel WHERE id = $1", id).StructScan(&c); err != nil {
			return nil, err
		}
		if c.PackageID.Valid {
			p, err := a.GetPackage(c.PackageID.String)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			c.Package = p
		}
		return &c, nil
	}
	sorted := func(c *types.Channel) *types.Channel {
		if c != nil && c.Package != nil {
			slices.Sort(c.Package.ChannelsBlacklist)
		}
		return c
	}
	same := func(label string, got *types.Channel, id null.String) {
		if !id.Valid {
			assert.Nil(t, got, label)
			return
		}
		want, err := mainChannel(id.String)
		require.NoError(t, err, label)
		assert.Equal(t, sorted(want), sorted(got), label)
	}

	for _, c := range channels {
		got, err := a.GetChannel(c.ID)
		require.NoError(t, err, c.Name)
		same("GetChannel "+c.Name, got, null.StringFrom(c.ID))
		for _, spelling := range []string{"{" + c.ID + "}", strings.ToUpper(c.ID), strings.ReplaceAll(c.ID, "-", "")} {
			got, err := a.GetChannel(spelling)
			require.NoError(t, err, spelling)
			same("GetChannel spelled "+spelling, got, null.StringFrom(c.ID))
		}
	}
	_, err = a.GetChannel(uuid.NewString())
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = a.GetChannel("not-a-uuid")
	require.Error(t, err)
	assert.NotErrorIs(t, err, sql.ErrNoRows)

	list, err := a.GetChannels(tApp.ID, 1, 100)
	require.NoError(t, err)
	var wantOrder []string
	require.NoError(t, a.db().Select(&wantOrder, "SELECT id FROM channel WHERE application_id = $1 ORDER BY name", tApp.ID))
	var gotOrder []string
	for _, c := range list {
		gotOrder = append(gotOrder, c.ID)
		same("GetChannels "+c.Name, c, null.StringFrom(c.ID))
	}
	assert.Equal(t, wantOrder, gotOrder, "GetChannels order")

	groups, err := a.GetGroups(tApp.ID, 1, 100)
	require.NoError(t, err)
	require.Len(t, groups, len(groupIDs))
	for _, g := range groups {
		same("GetGroups "+g.Name, g.Channel, g.ChannelID)
		one, err := a.GetGroup(g.ID)
		require.NoError(t, err)
		same("GetGroup "+g.Name, one.Channel, one.ChannelID)
	}

	// GetGroup parses the id as main did: other spellings of a uuid work, junk
	// fails, and an unknown id is not found.
	id := groupIDs[2]
	want, err := a.GetGroup(id)
	require.NoError(t, err)
	for _, spelling := range []string{"{" + id + "}", strings.ToUpper(id), strings.ReplaceAll(id, "-", "")} {
		got, err := a.GetGroup(spelling)
		require.NoError(t, err, spelling)
		assert.Equal(t, want, got, spelling)
	}
	for _, spelling := range []string{"urn:uuid:" + id, "not-a-uuid", ""} {
		_, err := a.GetGroup(spelling)
		require.Error(t, err, spelling)
		assert.NotErrorIs(t, err, sql.ErrNoRows, spelling)
	}
	_, err = a.GetGroup(uuid.NewString())
	require.ErrorIs(t, err, sql.ErrNoRows)

	app, err := a.GetApp(tApp.ID)
	require.NoError(t, err)
	require.Len(t, app.Groups, len(groupIDs))
	require.Len(t, app.Channels, len(channels))
	for _, g := range app.Groups {
		same("GetApp group "+g.Name, g.Channel, g.ChannelID)
	}
	for _, c := range app.Channels {
		same("GetApp channel "+c.Name, c, null.StringFrom(c.ID))
	}

	for i, c := range channels {
		id, err := a.GetGroupID(tApp.ID, "on "+c.Name, c.Arch)
		require.NoError(t, err, c.Name)
		assert.Equal(t, groupIDs[i+1], id, "GetGroupID "+c.Name)
	}
}
