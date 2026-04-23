package api_test

import (
	"testing"

	"gopkg.in/guregu/null.v4"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/flatcar/nebraska/backend/pkg/api"
)

func TestAddPackage(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tChannel1, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel1", Color: "blue", ApplicationID: tApp.ID, Arch: api.ArchAArch64})
	tChannel2, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel2", Color: "green", ApplicationID: tApp.ID, Arch: api.ArchAArch64})

	pkg, err := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID, ChannelsBlacklist: []string{tChannel1.ID, tChannel2.ID}, Arch: api.ArchAArch64})
	assert.NoError(t, err)

	_, err = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID, Arch: api.ArchX86})
	assert.NoError(t, err)

	pkgX, err := runtimeSvc(a).GetPackage(pkg.ID)
	assert.NoError(t, err)
	assert.Equal(t, api.PkgTypeOther, pkgX.Type)
	assert.Equal(t, "http://sample.url/pkg", pkgX.URL)
	assert.Equal(t, "12.1.0", pkgX.Version)
	assert.Equal(t, tApp.ID, pkgX.ApplicationID)
	assert.Contains(t, pkgX.ChannelsBlacklist, tChannel1.ID)
	assert.Contains(t, pkgX.ChannelsBlacklist, tChannel2.ID)
	assert.Equal(t, api.ArchAArch64, pkgX.Arch)

	_, err = adminSvc(a).AddPackage(&api.Package{URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	assert.Error(t, err, "Package type is required.")

	_, err = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, Version: "12.1.0", ApplicationID: tApp.ID})
	assert.Error(t, err, "Package url is required.")

	_, err = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", ApplicationID: tApp.ID})
	assert.Error(t, err, "Package version is required.")

	_, err = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "aaa12.1.0"})
	assert.Equal(t, api.ErrInvalidSemver, err, "Package version must be a valid semver.")

	_, err = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0"})
	assert.Error(t, err, "App id is required and must be a valid uuid.")

	_, err = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID, ChannelsBlacklist: []string{uuid.New().String()}})
	assert.Error(t, err, "Blacklisted channels must be existing channels ids.")

	_, err = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID, ChannelsBlacklist: []string{"invalidChannelID"}})
	assert.Error(t, err, "Blacklisted channels must be valid existing channels ids.")

	_, err = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0",
		ApplicationID: tApp.ID, ChannelsBlacklist: []string{tChannel1.ID}})
	assert.Equal(t, api.ErrArchMismatch, err, "When using Blacklisted channels, an Arch must be supplied.")

	_, err = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg",
		Version: "12.2.0", ApplicationID: tApp.ID,
		ChannelsBlacklist: []string{tChannel1.ID},
		Arch:              api.ArchAMD64})
	assert.Equal(t, api.ErrArchMismatch, err, "Blacklisted channels must have a matching arch.")

	_, err = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.3.0", ApplicationID: tApp.ID, Arch: api.Arch(77777)})
	assert.Error(t, err, "Arch must be a valid architecture")
}

func TestAddPackageFlatcar(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	pkg := &api.Package{
		Type:          api.PkgTypeFlatcar,
		URL:           "https://update.release.flatcar-linux.net/amd64-usr/XYZ/",
		Filename:      null.StringFrom("update.gz"),
		Version:       "2016.6.6",
		Size:          null.StringFrom("123456"),
		Hash:          null.StringFrom("sha1:blablablabla"),
		ApplicationID: api.FlatcarAppID,
		FlatcarAction: &api.FlatcarAction{
			Sha256: "sha256:blablablabla",
		},
	}
	_, err := adminSvc(a).AddPackage(pkg)
	assert.NoError(t, err)
	assert.Equal(t, "postinstall", pkg.FlatcarAction.Event)
	assert.Equal(t, false, pkg.FlatcarAction.NeedsAdmin)
	assert.Equal(t, false, pkg.FlatcarAction.IsDelta)
	assert.Equal(t, true, pkg.FlatcarAction.DisablePayloadBackoff)
	assert.Equal(t, "sha256:blablablabla", pkg.FlatcarAction.Sha256)
}

func TestUpdatePackage(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tChannel1, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel1", Color: "blue", ApplicationID: tApp.ID})
	tPkg, err := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID, ChannelsBlacklist: []string{tChannel1.ID}})
	assert.NoError(t, err)

	tChannel2, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel2", Color: "green", ApplicationID: tApp.ID})
	tChannel3, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel3", Color: "red", ApplicationID: tApp.ID})
	tChannel4, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel4", Color: "yellow", ApplicationID: tApp.ID, PackageID: null.StringFrom(tPkg.ID)})

	err = adminSvc(a).UpdatePackage(&api.Package{ID: tPkg.ID, Type: api.PkgTypeOther, URL: "http://sample.url/pkg_updated", Version: "12.2.0", ChannelsBlacklist: []string{tChannel2.ID, tChannel3.ID}})
	assert.NoError(t, err)

	pkg, err := runtimeSvc(a).GetPackage(tPkg.ID)
	assert.NoError(t, err)
	assert.Equal(t, "http://sample.url/pkg_updated", pkg.URL)
	assert.Equal(t, "12.2.0", pkg.Version)
	assert.NotContains(t, pkg.ChannelsBlacklist, tChannel1.ID)
	assert.Contains(t, pkg.ChannelsBlacklist, tChannel2.ID)
	assert.Contains(t, pkg.ChannelsBlacklist, tChannel3.ID)

	err = adminSvc(a).UpdatePackage(&api.Package{ID: tPkg.ID, Type: api.PkgTypeOther, URL: "http://sample.url/pkg_updated", Version: "12.2.0", ChannelsBlacklist: []string{tChannel4.ID}})
	assert.Equal(t, api.ErrBlacklistingChannel, err)

	err = adminSvc(a).UpdatePackage(&api.Package{ID: tPkg.ID, Type: api.PkgTypeOther, URL: "http://sample.url/pkg_updated", Version: "12.2.0", ChannelsBlacklist: nil, Arch: api.ArchAArch64})
	assert.NoError(t, err)
	pkg, _ = runtimeSvc(a).GetPackage(tPkg.ID)
	assert.Len(t, pkg.ChannelsBlacklist, 0)
	// can't change an arch of a package
	assert.Equal(t, api.ArchAll, pkg.Arch)
}

func TestUpdatePackageFlatcar(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	pkg := &api.Package{
		Type:          api.PkgTypeFlatcar,
		URL:           "https://update.release.flatcar-linux.net/amd64-usr/XYZ/",
		Filename:      null.StringFrom("update.gz"),
		Version:       "2016.6.6",
		Size:          null.StringFrom("123456"),
		Hash:          null.StringFrom("sha1:blablablabla"),
		ApplicationID: api.FlatcarAppID,
	}
	pkg, err := adminSvc(a).AddPackage(pkg)
	assert.NoError(t, err)
	assert.Nil(t, pkg.FlatcarAction)
	pkg.Version = "2016.6.7"
	err = adminSvc(a).UpdatePackage(pkg)
	assert.NoError(t, err)
	assert.Nil(t, pkg.FlatcarAction)

	pkg.FlatcarAction = &api.FlatcarAction{
		Sha256: "sha256:blablablabla",
	}
	err = adminSvc(a).UpdatePackage(pkg)
	assert.NoError(t, err)
	assert.Equal(t, "postinstall", pkg.FlatcarAction.Event)
	assert.Equal(t, false, pkg.FlatcarAction.NeedsAdmin)
	assert.Equal(t, false, pkg.FlatcarAction.IsDelta)
	assert.Equal(t, true, pkg.FlatcarAction.DisablePayloadBackoff)
	assert.Equal(t, "sha256:blablablabla", pkg.FlatcarAction.Sha256)

	err = adminSvc(a).DeletePackage(pkg.ID)
	assert.NoError(t, err)

	pkg = &api.Package{
		Type:          api.PkgTypeFlatcar,
		URL:           "https://update.release.flatcar-linux.net/amd64-usr/XYZ/",
		Filename:      null.StringFrom("update.gz"),
		Version:       "2016.6.6",
		Size:          null.StringFrom("123456"),
		Hash:          null.StringFrom("sha1:blablablabla"),
		ApplicationID: api.FlatcarAppID,
	}
	pkg.FlatcarAction = &api.FlatcarAction{
		Sha256: "sha256:blablablabla",
	}
	pkg, err = adminSvc(a).AddPackage(pkg)
	assert.NoError(t, err)
	assert.NotEqual(t, pkg.FlatcarAction.ID, "")

	flatcarActionID := pkg.FlatcarAction.ID
	pkg.FlatcarAction.Sha256 = "sha256:bleblebleble"
	err = adminSvc(a).UpdatePackage(pkg)
	assert.NoError(t, err)
	assert.Equal(t, "sha256:bleblebleble", pkg.FlatcarAction.Sha256)
	assert.Equal(t, flatcarActionID, pkg.FlatcarAction.ID)
}

func TestDeletePackage(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, err := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	assert.NoError(t, err)

	err = adminSvc(a).DeletePackage(tPkg.ID)
	assert.NoError(t, err)

	_, err = runtimeSvc(a).GetPackage(tPkg.ID)
	assert.Error(t, err, "Trying to get deleted package.")

	err = adminSvc(a).DeletePackage("invalidPackageID")
	assert.Error(t, err, "Package id must be a valid uuid.")
}

func TestGetPackage(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tChannel, _ := adminSvc(a).AddChannel(&api.Channel{Name: "test_channel1", Color: "blue", ApplicationID: tApp.ID})
	tPkg, err := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID, ChannelsBlacklist: []string{tChannel.ID}})
	assert.NoError(t, err)

	pkg, err := runtimeSvc(a).GetPackage(tPkg.ID)
	assert.NoError(t, err)
	assert.Equal(t, api.PkgTypeOther, pkg.Type)
	assert.Equal(t, "http://sample.url/pkg", pkg.URL)
	assert.Equal(t, "12.1.0", pkg.Version)
	assert.Equal(t, tApp.ID, pkg.ApplicationID)
	assert.Equal(t, api.StringArray([]string{tChannel.ID}), pkg.ChannelsBlacklist)
	assert.Equal(t, api.ArchAll, pkg.Arch)

	_, err = runtimeSvc(a).GetPackage("invalidPackageID")
	assert.Error(t, err, "Package id must be a valid uuid.")

	_, err = runtimeSvc(a).GetPackage(uuid.New().String())
	assert.Error(t, err, "Package id must exist.")
}

func TestGetPackageByVersionAndArch(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	tPkg, err := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "12.1.0", ApplicationID: tApp.ID})
	assert.NoError(t, err)
	tPkgARM, err := adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg", Version: "13.2.1", ApplicationID: tApp.ID, Arch: api.ArchAArch64})
	assert.NoError(t, err)

	pkg, err := runtimeSvc(a).GetPackageByVersionAndArch(tApp.ID, tPkg.Version, api.ArchAll)
	assert.NoError(t, err)
	assert.Equal(t, api.PkgTypeOther, pkg.Type)
	assert.Equal(t, "http://sample.url/pkg", pkg.URL)
	assert.Equal(t, "12.1.0", pkg.Version)
	assert.Equal(t, tApp.ID, pkg.ApplicationID)
	assert.Equal(t, api.ArchAll, pkg.Arch)

	_, err = runtimeSvc(a).GetPackageByVersionAndArch("invalidAppID", "12.1.0", api.ArchAll)
	assert.Error(t, err, "Application id must be a valid uuid.")

	_, err = runtimeSvc(a).GetPackageByVersionAndArch(uuid.New().String(), "12.1.0", api.ArchAll)
	assert.Error(t, err, "Application id must exist.")

	_, err = runtimeSvc(a).GetPackageByVersionAndArch(tApp.ID, "hola", api.ArchAll)
	assert.Error(t, err, "Version must be a valid semver value.")

	_, err = runtimeSvc(a).GetPackageByVersionAndArch(tApp.ID, tPkgARM.Version, api.ArchAll)
	assert.Error(t, err, "Shouldn't pick the ARM version")

	pkg, err = runtimeSvc(a).GetPackageByVersionAndArch(tApp.ID, tPkgARM.Version, api.ArchAArch64)
	assert.NoError(t, err)
	assert.Equal(t, api.ArchAArch64, pkg.Arch)
}

func TestGetPackages(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, _ := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	_, _ = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg1", Version: "1010.5.0+2016-05-27-1832", ApplicationID: tApp.ID, Arch: api.ArchAMD64})
	_, _ = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg2", Version: "12.1.0", ApplicationID: tApp.ID, Arch: api.ArchX86})
	_, _ = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg3", Version: "14.1.0", ApplicationID: tApp.ID, Arch: api.ArchAArch64})
	_, _ = adminSvc(a).AddPackage(&api.Package{Type: api.PkgTypeOther, URL: "http://sample.url/pkg4", Version: "1010.6.0-blabla", ApplicationID: tApp.ID})

	pkgs, err := runtimeSvc(a).GetPackages(tApp.ID, 0, 0, nil)
	assert.NoError(t, err)
	assert.Equal(t, 4, len(pkgs))
	assert.Equal(t, "http://sample.url/pkg4", pkgs[0].URL)
	assert.Equal(t, "http://sample.url/pkg1", pkgs[1].URL)
	assert.Equal(t, "http://sample.url/pkg3", pkgs[2].URL)
	assert.Equal(t, "http://sample.url/pkg2", pkgs[3].URL)

	assert.Equal(t, api.ArchAll, pkgs[0].Arch)
	assert.Equal(t, api.ArchAMD64, pkgs[1].Arch)
	assert.Equal(t, api.ArchAArch64, pkgs[2].Arch)
	assert.Equal(t, api.ArchX86, pkgs[3].Arch)

	_, err = runtimeSvc(a).GetPackages("invalidAppID", 0, 0, nil)
	assert.Error(t, err, "Add id must be a valid uuid.")

	_, err = runtimeSvc(a).GetPackages(uuid.New().String(), 0, 0, nil)
	assert.NoError(t, err, "should be no error for non existing appID")

	searchVersion := ".1.0"
	pkgs, err = runtimeSvc(a).GetPackages(tApp.ID, 0, 0, &searchVersion)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(pkgs))
}

func TestMultiFilePackage(t *testing.T) {
	a := newForTest(t)
	defer a.Close()

	tTeam, _ := adminSvc(a).AddTeam(&api.Team{Name: "test_team"})
	tApp, err := adminSvc(a).AddApp(&api.Application{Name: "test_app", TeamID: tTeam.ID})
	assert.NoError(t, err)

	pkg := &api.Package{
		Type:          api.PkgTypeOther,
		URL:           "https://myurl.io",
		Filename:      null.StringFrom("update.gz"),
		Version:       "1.2.3",
		Size:          null.StringFrom("123456"),
		Hash:          null.StringFrom("sha1:blablablabla"),
		ApplicationID: tApp.ID,
	}

	pkg, err = adminSvc(a).AddPackage(pkg)
	assert.NoError(t, err)
	assert.Nil(t, pkg.ExtraFiles)

	pkg.ExtraFiles = []api.File{
		{
			Name: null.StringFrom("myfile1.txt"),
			Size: null.StringFrom("1234"),
			Hash: null.StringFrom("abcd"),
		},
		{
			Name: null.StringFrom("myfile2.txt"),
			Size: null.StringFrom("12345"),
			Hash: null.StringFrom("abcde"),
		},
	}
	err = adminSvc(a).UpdatePackage(pkg)
	assert.NoError(t, err)

	oldFile1ID := pkg.ExtraFiles[0].ID

	pkg.ExtraFiles = []api.File{
		{
			Name: null.StringFrom("myfile1.txt"),
			Size: null.StringFrom(""),
			Hash: null.StringFrom("abcd"),
		},
		{
			Name: null.StringFrom("myfile2.txt"),
			Size: null.StringFrom("12345"),
			Hash: null.StringFrom("abcde"),
		},
	}
	err = adminSvc(a).UpdatePackage(pkg)
	assert.NoError(t, err)

	// Verify order after a lower-position file is updated.
	pkg, err = runtimeSvc(a).GetPackage(pkg.ID)
	assert.NoError(t, err)
	assert.NotEqual(t, oldFile1ID, pkg.ExtraFiles[0].ID)
	assert.Equal(t, "myfile1.txt", pkg.ExtraFiles[0].Name.String)
	assert.Equal(t, "abcd", pkg.ExtraFiles[0].Hash.String)
	assert.Equal(t, "abcde", pkg.ExtraFiles[1].Hash.String)
	assert.Equal(t, "", pkg.ExtraFiles[0].Size.String)
	assert.Equal(t, "12345", pkg.ExtraFiles[1].Size.String)

	// Modify same files
	oldFile1ID = pkg.ExtraFiles[0].ID

	// Switch file names without creating new files
	pkg.ExtraFiles = []api.File{
		{
			ID:   pkg.ExtraFiles[0].ID,
			Name: null.StringFrom("myfile2.txt"),
			Size: null.StringFrom(""),
			Hash: null.StringFrom("abcd"),
		},
		{
			ID:   pkg.ExtraFiles[1].ID,
			Name: null.StringFrom("myfile1.txt"),
			Size: null.StringFrom("12345"),
			Hash: null.StringFrom("abcde"),
		},
	}
	err = adminSvc(a).UpdatePackage(pkg)
	assert.NoError(t, err)

	// Verify order after a lower-position file is updated.
	pkg, err = runtimeSvc(a).GetPackage(pkg.ID)
	assert.NoError(t, err)
	assert.Equal(t, oldFile1ID, pkg.ExtraFiles[0].ID)
	assert.Equal(t, "myfile2.txt", pkg.ExtraFiles[0].Name.String)
	assert.Equal(t, "abcd", pkg.ExtraFiles[0].Hash.String)
	assert.Equal(t, "abcde", pkg.ExtraFiles[1].Hash.String)
	assert.Equal(t, "", pkg.ExtraFiles[0].Size.String)
	assert.Equal(t, "12345", pkg.ExtraFiles[1].Size.String)

	// Switch positions by recreating files
	pkg.ExtraFiles = []api.File{
		{
			Name: null.StringFrom("myfile2.txt"),
			Size: null.StringFrom("12345"),
			Hash: null.StringFrom("abcde"),
		},
		{
			Name: null.StringFrom("myfile1.txt"),
			Size: null.StringFrom(""),
			Hash: null.StringFrom("abcd"),
		},
	}
	err = adminSvc(a).UpdatePackage(pkg)
	assert.NoError(t, err)

	// Verify order after a lower-position file is updated.
	pkg, err = runtimeSvc(a).GetPackage(pkg.ID)
	assert.NoError(t, err)
	assert.Equal(t, "myfile2.txt", pkg.ExtraFiles[0].Name.String)
	assert.NotEqual(t, oldFile1ID, pkg.ExtraFiles[0].ID)

	pkg, err = runtimeSvc(a).GetPackage(pkg.ID)
	assert.NoError(t, err)
	assert.NotNil(t, pkg.ExtraFiles)
	assert.Equal(t, 2, len(pkg.ExtraFiles))

	pkg = &api.Package{
		Type:          api.PkgTypeOther,
		URL:           "https://myurl.io",
		Filename:      null.StringFrom("update.gz"),
		Version:       "1.2.33",
		Size:          null.StringFrom("123456"),
		Hash:          null.StringFrom("sha1:blablablabla"),
		ApplicationID: tApp.ID,
		ExtraFiles: []api.File{
			{
				Name: null.StringFrom("newfile1.txt"),
				Size: null.StringFrom("1234"),
				Hash: null.StringFrom("abcd"),
			},
			{
				Name: null.StringFrom("newfile2.txt"),
				Size: null.StringFrom("12345"),
				Hash: null.StringFrom("abcde"),
			},
		},
	}
	pkg, err = adminSvc(a).AddPackage(pkg)
	assert.NoError(t, err)
	assert.NotNil(t, pkg.ExtraFiles)
}
