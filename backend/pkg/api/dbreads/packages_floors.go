package dbreads

import (
	"database/sql"
	"fmt"

	"github.com/doug-martin/goqu/v9"
	"gopkg.in/guregu/null.v4"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/types"
)

// GetChannelFloorPackages returns all floor packages for a specific channel,
// ordered by version ascending.
func (q *Queries) GetChannelFloorPackages(channelID string) ([]*types.Package, error) {
	semverExpr, err := semverToIntArray("p.version")
	if err != nil {
		return nil, err
	}
	query, _, err := goqu.From(goqu.L(`
		package p
		JOIN channel_package_floors cpf ON p.id = cpf.package_id
	`)).
		Select(goqu.L(`
			p.*,
			true as is_floor,
			cpf.floor_reason
		`)).
		Where(goqu.C("channel_id").Table("cpf").Eq(channelID)).
		Order(goqu.L(semverExpr).Asc()).
		ToSQL()
	if err != nil {
		return nil, err
	}
	return q.getPackagesFromQuery(query)
}

// GetRequiredChannelFloors returns floor packages between the instance and
// target versions for the given channel. Capped at MaxFloorsPerResponse to
// avoid oversized syncer responses.
func (q *Queries) GetRequiredChannelFloors(channel *types.Channel, instanceVersion string) ([]*types.Package, error) {
	if channel == nil || channel.Package == nil {
		return nil, ErrNoPackageFound
	}
	if instanceVersion == "" {
		return nil, fmt.Errorf("instance version cannot be empty")
	}
	targetVersion := channel.Package.Version
	maxFloors := q.maxFloorsPerResponse
	if maxFloors <= 0 {
		maxFloors = DefaultMaxFloorsPerResponse
	}

	gtExpr, err := versionCompareExpr("p.version", ">", instanceVersion)
	if err != nil {
		return nil, err
	}
	lteExpr, err := versionCompareExpr("p.version", "<=", targetVersion)
	if err != nil {
		return nil, err
	}
	semverExpr, err := semverToIntArray("p.version")
	if err != nil {
		return nil, err
	}
	query, _, err := goqu.From(goqu.L(`
		package p
		JOIN channel_package_floors cpf ON p.id = cpf.package_id
	`)).
		Select(goqu.L(`
			p.*,
			true as is_floor,
			cpf.floor_reason
		`)).
		Where(goqu.And(
			goqu.C("channel_id").Table("cpf").Eq(channel.ID),
			gtExpr,
			lteExpr,
		)).
		Order(goqu.L(semverExpr).Asc()).
		Limit(uint(maxFloors)).
		ToSQL()
	if err != nil {
		return nil, err
	}
	return q.getPackagesFromQuery(query)
}

// GetChannelFloorPackagesCount returns the count of floor packages for a channel.
func (q *Queries) GetChannelFloorPackagesCount(channelID string) (int, error) {
	query := goqu.From("channel_package_floors").
		Where(goqu.C("channel_id").Eq(channelID)).
		Select(goqu.L("count(*)"))
	return q.GetCountQuery(query)
}

// GetChannelFloorPackagesPaginated returns paginated floor packages for a channel.
func (q *Queries) GetChannelFloorPackagesPaginated(channelID string, page, perPage uint64) ([]*types.Package, error) {
	page, perPage = shared.ValidatePaginationParams(page, perPage)
	limit, offset := shared.SQLPaginate(page, perPage)
	semverExpr, err := semverToIntArray("p.version")
	if err != nil {
		return nil, err
	}
	query, _, err := goqu.From(goqu.L(`
		package p
		JOIN channel_package_floors cpf ON p.id = cpf.package_id
	`)).
		Select(goqu.L(`
			p.*,
			true as is_floor,
			cpf.floor_reason
		`)).
		Where(goqu.C("channel_id").Table("cpf").Eq(channelID)).
		Order(goqu.L(semverExpr).Asc()).
		Limit(limit).
		Offset(offset).
		ToSQL()
	if err != nil {
		return nil, err
	}
	return q.getPackagesFromQuery(query)
}

// GetPackageFloorChannels returns every channel where the given package is
// marked as a floor, with the channel's current target package hydrated.
func (q *Queries) GetPackageFloorChannels(packageID string) ([]types.ChannelFloorInfo, error) {
	type channelWithFloor struct {
		types.Channel
		FloorReason null.String `db:"floor_reason"`
	}

	query, _, err := goqu.From(goqu.T("channel").As("c")).
		Join(goqu.T("channel_package_floors").As("cpf"), goqu.On(
			goqu.C("id").Table("c").Eq(goqu.C("channel_id").Table("cpf")),
		)).
		Select(
			goqu.C("id").Table("c"),
			goqu.C("name").Table("c"),
			goqu.C("color").Table("c"),
			goqu.C("created_ts").Table("c"),
			goqu.C("application_id").Table("c"),
			goqu.C("package_id").Table("c"),
			goqu.C("arch").Table("c"),
			goqu.C("floor_reason").Table("cpf"),
		).
		Where(goqu.C("package_id").Table("cpf").Eq(packageID)).
		Order(goqu.C("name").Table("c").Asc()).
		ToSQL()
	if err != nil {
		return nil, err
	}

	rows, err := q.db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []types.ChannelFloorInfo
	for rows.Next() {
		var chWithFloor channelWithFloor
		if err := rows.StructScan(&chWithFloor); err != nil {
			return nil, err
		}
		if chWithFloor.PackageID.Valid {
			pkg, err := q.getPackage(chWithFloor.PackageID)
			switch err {
			case nil:
				chWithFloor.Package = pkg
			case sql.ErrNoRows:
				chWithFloor.Package = nil
			default:
				return nil, err
			}
		}
		result = append(result, types.ChannelFloorInfo{
			Channel:     &chWithFloor.Channel,
			FloorReason: chWithFloor.FloorReason,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// IsPackageBlacklistedForChannel reports whether the given package is in
// the channel's blacklist. Used internally by floor writers (and exported
// here so admin writers in a later phase can reach it across the package
// boundary).
func (q *Queries) IsPackageBlacklistedForChannel(packageID, channelID string) (bool, error) {
	query, _, err := goqu.From("package_channel_blacklist").
		Select(goqu.COUNT("*")).
		Where(goqu.And(
			goqu.C("channel_id").Eq(channelID),
			goqu.C("package_id").Eq(packageID),
		)).
		ToSQL()
	if err != nil {
		return false, err
	}
	var count int
	if err := q.db.QueryRow(query).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// IsPackageFloorForChannel reports whether the given package is currently a
// floor for the channel.
func (q *Queries) IsPackageFloorForChannel(packageID, channelID string) (bool, error) {
	query, _, err := goqu.From("channel_package_floors").
		Select(goqu.COUNT("*")).
		Where(goqu.And(
			goqu.C("channel_id").Eq(channelID),
			goqu.C("package_id").Eq(packageID),
		)).
		ToSQL()
	if err != nil {
		return false, err
	}
	var count int
	if err := q.db.QueryRow(query).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
