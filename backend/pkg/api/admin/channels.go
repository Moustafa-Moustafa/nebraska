package admin

import (
	"github.com/doug-martin/goqu/v9"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

// AddChannel registers the provided channel.
func (s *Service) AddChannel(channel *api.Channel) (*api.Channel, error) {
	if !channel.Arch.IsValid() {
		return nil, api.ErrInvalidArch
	}
	if channel.PackageID.String != "" {
		if _, err := s.validatePackage(channel.PackageID.String, channel.ID, channel.ApplicationID, channel.Arch); err != nil {
			return nil, err
		}
	}
	query, _, err := goqu.Insert("channel").
		Cols("name", "color", "application_id", "package_id", "arch").
		Vals(goqu.Vals{
			channel.Name,
			channel.Color,
			channel.ApplicationID,
			channel.PackageID,
			channel.Arch}).
		Returning(goqu.T("channel").All()).
		ToSQL()
	if err != nil {
		return nil, err
	}
	err = s.db.QueryRowx(query).StructScan(channel)
	if err != nil {
		return nil, err
	}
	return channel, nil
}

// UpdateChannel updates an existing channel.
func (s *Service) UpdateChannel(channel *api.Channel) error {
	channelBeforeUpdate, err := s.GetChannel(channel.ID)
	if err != nil {
		return err
	}

	var pkg *api.Package
	if channel.PackageID.String != "" {
		if pkg, err = s.validatePackage(channel.PackageID.String, channel.ID, channelBeforeUpdate.ApplicationID, channelBeforeUpdate.Arch); err != nil {
			return err
		}
	}
	query, _, err := goqu.Update("channel").
		Set(goqu.Record{
			"name":       channel.Name,
			"color":      channel.Color,
			"package_id": channel.PackageID,
		}).
		Where(goqu.C("id").Eq(channel.ID)).
		ToSQL()
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

	if channelBeforeUpdate.PackageID.String != channel.PackageID.String && pkg != nil {
		if err := s.newAdminActivityEntry(shared.ActivityInfo, pkg.Version, pkg.ApplicationID, channel.ID); err != nil {
			l.Error().Err(err).Msg("UpdateChannel - could not add admin activity entry")
		}
	}

	return nil
}

// DeleteChannel removes the channel identified by the id provided.
func (s *Service) DeleteChannel(channelID string) error {
	query, _, err := goqu.Delete("channel").
		Where(goqu.C("id").Eq(channelID)).
		ToSQL()
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

// validatePackage checks that a package is valid for use in a channel:
// it must belong to the same application, have a matching arch, and not
// have blacklisted the channel.
func (s *Service) validatePackage(packageID, channelID, appID string, channelArch api.Arch) (*api.Package, error) {
	pkg, err := s.GetPackage(packageID)
	if err == nil {
		if pkg.ApplicationID != appID {
			return nil, api.ErrInvalidPackage
		}
		if pkg.Arch != channelArch {
			return nil, api.ErrArchMismatch
		}
		for _, blacklistedChannelID := range pkg.ChannelsBlacklist {
			if channelID == blacklistedChannelID {
				return nil, api.ErrBlacklistedChannel
			}
		}
	}
	return pkg, err
}

// newAdminActivityEntry creates an admin_activity entry for channel package updates.
func (s *Service) newAdminActivityEntry(severity int, version, appID, channelID string) error {
	query, _, err := goqu.Insert("admin_activity").
		Cols("severity", "version", "application_id", "channel_id").
		Vals(goqu.Vals{severity, version, appID, channelID}).
		ToSQL()
	if err != nil {
		return err
	}
	_, err = s.db.Exec(query)
	return err
}
