package api

import (
	"errors"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/jmoiron/sqlx"
	"gopkg.in/guregu/null.v4"
)

const (
	// PkgTypeFlatcar indicates that the package is a Flatcar update package
	PkgTypeFlatcar int = 1 + iota

	// PkgTypeDocker indicates that the package is a Docker container
	PkgTypeDocker

	// PkgTypeRocket indicates that the package is a Rocket container
	PkgTypeRocket

	// PkgTypeOther is the generic package type.
	PkgTypeOther
)

var (
	// ErrBlacklistingChannel error indicates that the channel the package is
	// trying to blacklist is already pointing to the package.
	ErrBlacklistingChannel = errors.New("nebraska: channel trying to blacklist is already pointing to the package")
)

type File struct {
	ID        int64       `db:"id" json:"id"`
	PackageID string      `db:"package_id" json:"package_id"`
	Name      null.String `db:"name" json:"name"`
	Size      null.String `db:"size" json:"size"`
	Hash      null.String `db:"hash" json:"hash"`
	Hash256   null.String `db:"hash256" json:"hash256"`
	CreatedTs time.Time   `db:"created_ts" json:"created_ts"`
}

func (f File) Equals(otherFile File) bool {
	return f.Name.String == otherFile.Name.String && f.Size.String == otherFile.Size.String && f.Hash.String == otherFile.Hash.String && f.Hash256.String == otherFile.Hash256.String
}

// Package represents a Nebraska application's package.
type Package struct {
	ID                string         `db:"id" json:"id"`
	Type              int            `db:"type" json:"type"`
	Version           string         `db:"version" json:"version"`
	URL               string         `db:"url" json:"url"`
	Filename          null.String    `db:"filename" json:"filename"`
	Description       null.String    `db:"description" json:"description"`
	Size              null.String    `db:"size" json:"size"`
	Hash              null.String    `db:"hash" json:"hash"`
	CreatedTs         time.Time      `db:"created_ts" json:"created_ts"`
	ChannelsBlacklist StringArray    `db:"channels_blacklist" json:"channels_blacklist"`
	ApplicationID     string         `db:"application_id" json:"application_id"`
	FlatcarAction     *FlatcarAction `db:"flatcar_action" json:"flatcar_action"`
	Arch              Arch           `db:"arch" json:"arch"`
	ExtraFiles        []File         `db:"extra_files" json:"extra_files"`
}

// ChannelReader is the minimal interface needed by UpdatePackageBlacklistedChannels.
type ChannelReader interface {
	GetChannel(channelID string) (*Channel, error)
}

// UpdatePackageBlacklistedChannels manages the package-channel blacklist
// within a transaction. Exported for use by the admin sub-package.
func UpdatePackageBlacklistedChannels(tx *sqlx.Tx, pkg *Package, oldPkg *Package, reader ChannelReader) error {
	newBL := make(map[string]struct{}, len(pkg.ChannelsBlacklist))
	for _, ch := range pkg.ChannelsBlacklist {
		newBL[ch] = struct{}{}
	}
	oldBL := make(map[string]struct{}, len(oldPkg.ChannelsBlacklist))
	for _, ch := range oldPkg.ChannelsBlacklist {
		oldBL[ch] = struct{}{}
	}
	for ch := range newBL {
		if _, ok := oldBL[ch]; ok {
			continue
		}
		channel, err := reader.GetChannel(ch)
		if err != nil {
			return err
		}
		if channel.PackageID.String == pkg.ID {
			return ErrBlacklistingChannel
		}
		q, _, err := goqu.Insert("package_channel_blacklist").Cols("package_id", "channel_id").Vals(goqu.Vals{pkg.ID, ch}).ToSQL()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	for ch := range oldBL {
		if _, ok := newBL[ch]; ok {
			continue
		}
		q, _, err := goqu.Delete("package_channel_blacklist").Where(goqu.C("package_id").Eq(pkg.ID), goqu.C("channel_id").Eq(ch)).ToSQL()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// UpdatePackageFiles manages package extra files within a transaction.
// Exported for use by the admin sub-package.
func UpdatePackageFiles(tx *sqlx.Tx, pkg *Package, oldPkg *Package) error {
	var oldFiles map[int64]File
	if oldPkg != nil {
		oldFiles = make(map[int64]File, len(oldPkg.ExtraFiles))
		for _, f := range oldPkg.ExtraFiles {
			oldFiles[f.ID] = f
		}
	}
	for _, nf := range pkg.ExtraFiles {
		isUpdate := false
		if of, ok := oldFiles[nf.ID]; ok {
			if of.ID == nf.ID {
				delete(oldFiles, nf.ID)
				if of.Equals(nf) {
					continue
				}
				isUpdate = true
			}
		}
		if isUpdate {
			q, _, err := goqu.Update("package_file").Set(goqu.Record{"name": nf.Name.String, "size": nf.Size.String, "hash": nf.Hash.String, "hash256": nf.Hash256.String}).Where(goqu.C("id").Eq(nf.ID)).ToSQL()
			if err != nil {
				return err
			}
			if _, err = tx.Exec(q); err != nil {
				return err
			}
			continue
		}
		q, _, err := goqu.Insert("package_file").Cols("package_id", "name", "size", "hash", "hash256").Vals(goqu.Vals{pkg.ID, nf.Name.String, nf.Size.String, nf.Hash.String, nf.Hash256.String}).ToSQL()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	for id := range oldFiles {
		of := oldFiles[id]
		q, _, err := goqu.Delete("package_file").Where(goqu.C("package_id").Eq(pkg.ID), goqu.C("id").Eq(of.ID)).ToSQL()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	return nil
}
