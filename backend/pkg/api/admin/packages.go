package admin

import (
	"database/sql"

	"github.com/doug-martin/goqu/v9"
	"github.com/google/uuid"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

// AddPackage registers the provided package.
func (s *Service) AddPackage(pkg *api.Package) (*api.Package, error) {
	if !shared.IsValidSemver(pkg.Version) {
		return nil, api.ErrInvalidSemver
	}
	if !pkg.Arch.IsValid() {
		return nil, api.ErrInvalidArch
	}
	if err := s.checkMatchingArch(pkg.ChannelsBlacklist, pkg.Arch); err != nil {
		return nil, err
	}

	tx, err := s.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			l.Error().Err(err).Msg("AddPackage - could not roll back")
		}
	}()

	query, _, err := goqu.Insert("package").
		Cols("type", "filename", "description", "size", "hash", "url", "version", "application_id", "arch").
		Vals(goqu.Vals{
			pkg.Type,
			pkg.Filename,
			pkg.Description,
			pkg.Size,
			pkg.Hash,
			pkg.URL,
			pkg.Version,
			pkg.ApplicationID,
			pkg.Arch,
		}).
		Returning(goqu.T("package").All()).
		ToSQL()
	if err != nil {
		return nil, err
	}
	err = tx.QueryRowx(query).StructScan(pkg)
	if err != nil {
		return nil, err
	}
	if len(pkg.ChannelsBlacklist) > 0 {
		for _, channelID := range pkg.ChannelsBlacklist {
			q, _, err := goqu.Insert("package_channel_blacklist").
				Cols("package_id", "channel_id").
				Vals(goqu.Vals{pkg.ID, channelID}).
				ToSQL()
			if err != nil {
				return nil, err
			}
			if _, err = tx.Exec(q); err != nil {
				return nil, err
			}
		}
	}

	if err = api.UpdatePackageFiles(tx, pkg, nil); err != nil {
		return nil, err
	}

	if pkg.Type == api.PkgTypeFlatcar && pkg.FlatcarAction != nil {
		q, _, err := goqu.Insert("flatcar_action").
			Cols("package_id", "sha256").
			Vals(goqu.Vals{pkg.ID, pkg.FlatcarAction.Sha256}).
			Returning(goqu.T("flatcar_action").All()).
			ToSQL()
		if err != nil {
			return nil, err
		}
		flatcarAction := &api.FlatcarAction{}
		err = tx.QueryRowx(q).StructScan(flatcarAction)
		switch err {
		case nil:
			pkg.FlatcarAction = flatcarAction
		case sql.ErrNoRows:
			pkg.FlatcarAction = nil
		default:
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return pkg, nil
}

// UpdatePackage updates an existing package.
func (s *Service) UpdatePackage(pkg *api.Package) error {
	if !shared.IsValidSemver(pkg.Version) {
		return api.ErrInvalidSemver
	}
	tx, err := s.db.Beginx()
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			l.Error().Err(err).Msg("UpdatePackage - could not roll back")
		}
	}()
	query, _, err := goqu.Update("package").
		Set(goqu.Record{
			"type":        pkg.Type,
			"filename":    pkg.Filename,
			"description": pkg.Description,
			"size":        pkg.Size,
			"hash":        pkg.Hash,
			"url":         pkg.URL,
			"version":     pkg.Version,
		}).
		Where(goqu.C("id").Eq(pkg.ID)).
		ToSQL()
	if err != nil {
		return err
	}
	result, err := tx.Exec(query)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	} else if rowsAffected == 0 {
		return api.ErrNoRowsAffected
	}

	oldPkg, err := s.GetPackage(pkg.ID)
	if err != nil {
		return err
	}

	if err = api.UpdatePackageBlacklistedChannels(tx, pkg, oldPkg, &s.Queries); err != nil {
		return err
	}

	if err = api.UpdatePackageFiles(tx, pkg, oldPkg); err != nil {
		return err
	}

	if pkg.Type == api.PkgTypeFlatcar && pkg.FlatcarAction != nil {
		if pkg.FlatcarAction.ID == "" {
			pkg.FlatcarAction.ID = uuid.New().String()
		}
		q, _, err := goqu.Insert("flatcar_action").
			Cols("id", "package_id", "sha256").
			Vals(goqu.Vals{pkg.FlatcarAction.ID, pkg.ID, pkg.FlatcarAction.Sha256}).
			OnConflict(goqu.DoUpdate("id", goqu.Record{"sha256": pkg.FlatcarAction.Sha256, "package_id": pkg.ID})).
			Returning(goqu.T("flatcar_action").All()).
			ToSQL()
		if err != nil {
			return err
		}
		err = tx.QueryRowx(q).StructScan(pkg.FlatcarAction)
		if err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// DeletePackage removes the package identified by the id provided.
func (s *Service) DeletePackage(pkgID string) error {
	query, _, err := goqu.Delete("package").Where(goqu.C("id").Eq(pkgID)).ToSQL()
	if err != nil {
		return err
	}
	result, err := s.db.Exec(query)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return api.ErrNoRowsAffected
	}
	return nil
}

// checkMatchingArch verifies that all blacklisted channels have the same arch
// as the package being added.
func (s *Service) checkMatchingArch(channelIDs api.StringArray, arch api.Arch) error {
	if len(channelIDs) == 0 {
		return nil
	}

	query, _, err := goqu.From("channel").
		Select(goqu.COUNT("*")).
		Where(goqu.Ex{"id": channelIDs}).
		Where(goqu.C("arch").Neq(arch)).
		ToSQL()

	if err != nil {
		return err
	}
	count := 0
	if err := s.db.QueryRow(query).Scan(&count); err != nil {
		return err
	}

	if count > 0 {
		return api.ErrArchMismatch
	}
	return nil
}
