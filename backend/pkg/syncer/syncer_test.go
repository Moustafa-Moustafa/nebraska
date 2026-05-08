package syncer

import (
	"log"
	"os"
	"testing"

	"github.com/flatcar/go-omaha/omaha"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/guregu/null.v4"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/admin"
	"github.com/flatcar/nebraska/backend/pkg/api/dbreads"
)

const (
	defaultTestDbURL string = "postgres://postgres:nebraska@127.0.0.1:5432/nebraska_tests?sslmode=disable&connect_timeout=10"
)

func newAPI(t *testing.T) *api.API {
	t.Helper()
	a, err := api.NewForTest(api.OptionInitDB, api.OptionDisableUpdatesOnFailedRollout)

	t.Logf("Failed to init DB: %v\n", err)
	t.Log("These tests require PostgreSQL running and a tests database created, please adjust NEBRASKA_DB_URL as needed.")
	require.NoError(t, err)

	return a
}

func newForTest(t *testing.T, conf *Config) *Syncer {
	t.Helper()
	a := newAPI(t)

	if conf.Queries == nil {
		conf.Queries = dbreads.New(a.DB())
	}
	if conf.Admin == nil {
		conf.Admin = admin.NewService(a.DB())
	}
	if conf.Closer == nil {
		conf.Closer = a
	}
	s, err := New(conf)
	require.NoError(t, err)

	return s
}

func queries(a *api.API) *dbreads.Queries {
	return dbreads.New(a.DB())
}

func TestMain(m *testing.M) {
	if os.Getenv("NEBRASKA_SKIP_TESTS") != "" {
		return
	}

	if _, ok := os.LookupEnv("NEBRASKA_DB_URL"); !ok {
		log.Printf("NEBRASKA_DB_URL not set, setting to default %q\n", defaultTestDbURL)
		_ = os.Setenv("NEBRASKA_DB_URL", defaultTestDbURL)
	}

	os.Exit(m.Run())
}

func TestSyncer_NoAPI(t *testing.T) {
	_, err := New(&Config{})
	assert.ErrorIs(t, err, ErrInvalidAPIInstance)
}

func TestSyncer_InvalidPkgsURL(t *testing.T) {
	a := newAPI(t)
	t.Cleanup(func() {
		a.Close()
	})

	tests := []struct {
		url   string
		isErr bool
	}{
		{
			url:   "",
			isErr: false,
		},
		{
			url:   ":file",
			isErr: true,
		},
		{
			url:   "https://myphony.url",
			isErr: false,
		},
		{
			url:   "file:///my/file",
			isErr: false,
		},
	}

	for _, tc := range tests {
		testCase := tc
		t.Run(testCase.url, func(t *testing.T) {
			t.Parallel()

			_, err := New(&Config{
				Queries:     dbreads.New(a.DB()),
				Admin:       admin.NewService(a.DB()),
				Closer:      a,
				PackagesURL: testCase.url,
			})
			if testCase.isErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSyncer_Init(t *testing.T) {
	syncer := newForTest(t, &Config{})
	a := syncer.queries
	t.Cleanup(func() {
		syncer.closer.Close()
	})

	tApp, err := a.GetApp(flatcarAppID)
	require.NoError(t, err)
	tPkg, err := syncer.admin.AddPackage(&api.Package{Type: api.PkgTypeFlatcar, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID, Arch: api.ArchAMD64})
	require.NoError(t, err)
	groupID, err := a.GetGroupID(flatcarAppID, "stable", tPkg.Arch)
	require.NoError(t, err)

	tGroup, err := a.GetGroup(groupID)
	require.NoError(t, err)

	tChannel := tGroup.Channel

	tChannel.PackageID = null.StringFrom(tPkg.ID)

	err = syncer.admin.UpdateChannel(tChannel)
	require.NoError(t, err)

	err = syncer.initialize()
	require.NoError(t, err)

	desc := channelDescriptor{
		name: tChannel.Name,
		arch: tChannel.Arch,
	}

	version, ok := syncer.versions[desc]
	assert.True(t, ok)
	assert.Equal(t, tPkg.Version, version)
}

func createOmahaUpdate() *omaha.UpdateResponse {
	return &omaha.UpdateResponse{
		URLs: []*omaha.URL{
			{CodeBase: "https://example.com"},
		},
		Manifest: &omaha.Manifest{
			Version: "1.2.3",
			Packages: []*omaha.Package{
				{
					Name: "updatepayload.tgz",
					SHA1: "00000000000000000",
				},
				{
					Name: "extra-file1.tgz",
					SHA1: "00000000000000001",
				},
				{
					Name: "extra-file2.tgz",
					SHA1: "00000000000000002",
				},
			},
			Actions: []*omaha.Action{
				{},
			},
		},
	}
}

func setupFlatcarAppStableGroup(t *testing.T, a *api.API) *api.Group {
	t.Helper()
	q := dbreads.New(a.DB())
	tApp, err := q.GetApp(flatcarAppID)
	require.NoError(t, err)
	tPkg, err := admin.NewService(a.DB()).AddPackage(&api.Package{Type: api.PkgTypeFlatcar, URL: "http://sample.url/pkg", Version: "0.1.0", ApplicationID: tApp.ID, Arch: api.ArchAMD64})
	require.NoError(t, err)
	groupID, err := q.GetGroupID(flatcarAppID, "stable", tPkg.Arch)
	require.NoError(t, err)

	tGroup, err := q.GetGroup(groupID)
	require.NoError(t, err)

	tChannel := tGroup.Channel

	tChannel.PackageID = null.StringFrom(tPkg.ID)

	return tGroup
}

func TestSyncer_GetPackage(t *testing.T) {
	syncer := newForTest(t, &Config{})
	apiDB := newAPI(t)
	t.Cleanup(func() {
		syncer.closer.Close()
	})

	tGroup := setupFlatcarAppStableGroup(t, apiDB)
	tChannel := tGroup.Channel

	err := syncer.initialize()
	require.NoError(t, err)

	update := createOmahaUpdate()

	desc := channelDescriptor{
		name: tChannel.Name,
		arch: tChannel.Arch,
	}
	err = syncer.processUpdate(desc, update)
	require.NoError(t, err)

	// Get updated group
	tGroup, err = queries(apiDB).GetGroup(tGroup.ID)
	require.NoError(t, err)

	assert.Equal(t, update.Manifest.Version, tGroup.Channel.Package.Version)
	assert.Equal(t, update.URLs[0].CodeBase, tGroup.Channel.Package.URL)
	assert.Equal(t, update.Manifest.Packages[0].Name, tGroup.Channel.Package.Filename.String)
}

func TestSyncer_GetMultiFilePackage(t *testing.T) {
	syncer := newForTest(t, &Config{})
	apiDB := newAPI(t)
	t.Cleanup(func() {
		syncer.closer.Close()
	})

	tGroup := setupFlatcarAppStableGroup(t, apiDB)
	tChannel := tGroup.Channel

	err := syncer.initialize()
	require.NoError(t, err)

	update := createOmahaUpdate()

	desc := channelDescriptor{
		name: tChannel.Name,
		arch: tChannel.Arch,
	}
	err = syncer.processUpdate(desc, update)
	require.NoError(t, err)

	// Get updated group
	tGroup, err = queries(apiDB).GetGroup(tGroup.ID)
	require.NoError(t, err)

	assert.Equal(t, update.Manifest.Version, tGroup.Channel.Package.Version)
	assert.Equal(t, update.URLs[0].CodeBase, tGroup.Channel.Package.URL)
	assert.Equal(t, update.Manifest.Packages[0].Name, tGroup.Channel.Package.Filename.String)
	assert.Equal(t, len(update.Manifest.Packages), len(tGroup.Channel.Package.ExtraFiles)+1)
	assert.Equal(t, update.Manifest.Packages[1].Name, tGroup.Channel.Package.ExtraFiles[0].Name.String)
	assert.Equal(t, update.Manifest.Packages[2].Name, tGroup.Channel.Package.ExtraFiles[1].Name.String)
}

func TestSyncer_GetPackageWithDiffURL(t *testing.T) {
	conf := &Config{
		PackagesURL: "https://my.super.different.packagesurl.io/bucket/",
	}
	syncer := newForTest(t, conf)
	apiDB := newAPI(t)
	t.Cleanup(func() {
		syncer.closer.Close()
	})

	tGroup := setupFlatcarAppStableGroup(t, apiDB)
	tChannel := tGroup.Channel

	err := syncer.initialize()
	require.NoError(t, err)

	update := createOmahaUpdate()

	desc := channelDescriptor{
		name: tChannel.Name,
		arch: tChannel.Arch,
	}
	err = syncer.processUpdate(desc, update)
	require.NoError(t, err)

	// Get updated group
	tGroup, err = queries(apiDB).GetGroup(tGroup.ID)
	require.NoError(t, err)

	assert.Equal(t, update.Manifest.Version, tGroup.Channel.Package.Version)
	assert.Equal(t, conf.PackagesURL, tGroup.Channel.Package.URL)
	assert.Equal(t, update.Manifest.Packages[0].Name, tGroup.Channel.Package.Filename.String)
}

func TestSyncer_GetPackageWithGeneratedURL(t *testing.T) {
	baseURL := "https://my.super.different.packagesurl.io/bucket/"
	conf := &Config{
		PackagesURL: baseURL + "{{ARCH}}/{{VERSION}}",
	}
	syncer := newForTest(t, conf)
	apiDB := newAPI(t)
	t.Cleanup(func() {
		syncer.closer.Close()
	})

	tGroup := setupFlatcarAppStableGroup(t, apiDB)
	tChannel := tGroup.Channel

	err := syncer.initialize()
	require.NoError(t, err)

	update := createOmahaUpdate()

	desc := channelDescriptor{
		name: tChannel.Name,
		arch: tChannel.Arch,
	}
	err = syncer.processUpdate(desc, update)
	require.NoError(t, err)

	// Get updated group
	tGroup, err = queries(apiDB).GetGroup(tGroup.ID)
	require.NoError(t, err)

	assert.Equal(t, update.Manifest.Version, tGroup.Channel.Package.Version)
	assert.Equal(t, baseURL+getArchString(tChannel.Arch)+"/"+tGroup.Channel.Package.Version, tGroup.Channel.Package.URL)
	assert.Equal(t, update.Manifest.Packages[0].Name, tGroup.Channel.Package.Filename.String)
}
