package api

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"

	"github.com/doug-martin/goqu/v9"
	"gopkg.in/guregu/null.v4"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/types"
)

// ChannelFloorInfo is owned by pkg/api/internal/types; re-exported here.
type ChannelFloorInfo = types.ChannelFloorInfo

var (
	// ErrPackageBlacklisted indicates that the package is blacklisted for this channel
	ErrPackageBlacklisted = errors.New("nebraska: cannot mark blacklisted package as floor")
)

// semverToIntArray returns a PostgreSQL expression that converts a semantic version
// to an integer array for proper version comparison.
// Handles versions like "1.2.3", "1.2.3-beta", "1.2.3+build"
// The column parameter must be a safe SQL identifier (no user input!)
func semverToIntArray(column string) (string, error) {
	if column != "?" && !regexp.MustCompile(`^[a-z_]+(\.[a-z_]+)?$`).MatchString(column) {
		return "", fmt.Errorf("semverToIntArray: invalid column name %q - potential SQL injection", column)
	}
	return fmt.Sprintf("string_to_array((regexp_split_to_array(%s, '[+-]'))[1], '.')::int[]", column), nil
}

// versionCompareExpr creates a version comparison expression
func versionCompareExpr(column, operator, value string) (goqu.Expression, error) {
	// Validate operator to prevent SQL injection
	validOperators := map[string]bool{
		">": true, ">=": true, "<": true, "<=": true, "=": true, "!=": true,
	}
	if !validOperators[operator] {
		return nil, fmt.Errorf("versionCompareExpr: invalid operator %q - potential SQL injection", operator)
	}

	colArray, err := semverToIntArray(column)
	if err != nil {
		return nil, err
	}

	valArray, err := semverToIntArray("?")
	if err != nil {
		return nil, err
	}

	return goqu.L(fmt.Sprintf("%s %s %s", colArray, operator, valArray), value), nil
}

// AddChannelPackageFloor marks a package as a floor for a specific channel
func (api *API) AddChannelPackageFloor(channelID, packageID string, floorReason null.String) error {
	// Verify channel and package exist and are compatible in a single query
	var channelArch, pkgArch Arch
	var channelAppID, pkgAppID string

	query, _, err := goqu.From(goqu.T("channel").As("c")).
		CrossJoin(goqu.T("package").As("p")).
		Select(
			goqu.C("arch").Table("c"),
			goqu.C("application_id").Table("c"),
			goqu.C("arch").Table("p"),
			goqu.C("application_id").Table("p"),
		).
		Where(goqu.And(
			goqu.C("id").Table("c").Eq(channelID),
			goqu.C("id").Table("p").Eq(packageID),
		)).
		ToSQL()

	if err != nil {
		return err
	}

	err = api.db.QueryRow(query).Scan(&channelArch, &channelAppID, &pkgArch, &pkgAppID)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrInvalidPackage
		}
		return err
	}

	// Verify architecture and application match
	if channelArch != pkgArch {
		return ErrArchMismatch
	}
	if channelAppID != pkgAppID {
		return ErrInvalidApplicationOrGroup
	}

	// Check if package is blacklisted for this channel
	isBlacklisted, err := api.isPackageBlacklistedForChannel(packageID, channelID)
	if err != nil {
		return err
	}
	if isBlacklisted {
		return ErrPackageBlacklisted
	}

	query, _, err = goqu.Insert("channel_package_floors").
		Cols("channel_id", "package_id", "floor_reason").
		Vals(goqu.Vals{channelID, packageID, floorReason}).
		OnConflict(goqu.DoUpdate("channel_id, package_id", goqu.Record{"floor_reason": floorReason})).
		ToSQL()

	if err != nil {
		return err
	}

	_, err = api.db.Exec(query)
	return err
}

// isPackageBlacklistedForChannel forwards to api.queries.
func (api *API) isPackageBlacklistedForChannel(packageID, channelID string) (bool, error) {
	return api.queries.IsPackageBlacklistedForChannel(packageID, channelID)
}

// isPackageFloorForChannel forwards to api.queries.
func (api *API) isPackageFloorForChannel(packageID, channelID string) (bool, error) {
	return api.queries.IsPackageFloorForChannel(packageID, channelID)
}

// RemoveChannelPackageFloor removes a package from being a floor for a specific channel
func (api *API) RemoveChannelPackageFloor(channelID, packageID string) error {
	query, _, err := goqu.Delete("channel_package_floors").
		Where(goqu.And(
			goqu.C("channel_id").Eq(channelID),
			goqu.C("package_id").Eq(packageID),
		)).
		ToSQL()

	if err != nil {
		return err
	}

	result, err := api.db.Exec(query)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrNoRowsAffected
	}

	return nil
}

// DefaultMaxFloorsPerResponse is the default maximum number of floor versions
// to return in a single update response. Mirrors dbreads.DefaultMaxFloorsPerResponse
// for external callers that still reference it via api.
const DefaultMaxFloorsPerResponse = 5

// GetChannelFloorPackages forwards to api.queries.
func (api *API) GetChannelFloorPackages(channelID string) ([]*Package, error) {
	return api.queries.GetChannelFloorPackages(channelID)
}

// GetRequiredChannelFloors forwards to api.queries.
func (api *API) GetRequiredChannelFloors(channel *Channel, instanceVersion string) ([]*Package, error) {
	return api.queries.GetRequiredChannelFloors(channel, instanceVersion)
}

// GetChannelFloorPackagesCount forwards to api.queries.
func (api *API) GetChannelFloorPackagesCount(channelID string) (int, error) {
	return api.queries.GetChannelFloorPackagesCount(channelID)
}

// GetChannelFloorPackagesPaginated forwards to api.queries.
func (api *API) GetChannelFloorPackagesPaginated(channelID string, page, perPage uint64) ([]*Package, error) {
	return api.queries.GetChannelFloorPackagesPaginated(channelID, page, perPage)
}

// GetPackageFloorChannels forwards to api.queries.
func (api *API) GetPackageFloorChannels(packageID string) ([]ChannelFloorInfo, error) {
	return api.queries.GetPackageFloorChannels(packageID)
}
