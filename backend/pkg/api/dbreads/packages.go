package dbreads

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/doug-martin/goqu/v9"
	"gopkg.in/guregu/null.v4"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

func (q *Queries) GetPackage(pkgID string) (*api.Package, error) {
	return q.getPackage(null.StringFrom(pkgID))
}

func (q *Queries) GetPackageByVersionAndArch(appID, version string, arch api.Arch) (*api.Package, error) {
	var pkg api.Package
	if !shared.IsValidSemver(version) {
		return nil, fmt.Errorf("error GetPackageByVersionAndArch version %s is not valid", version)
	}
	query, _, err := q.packagesQuery().
		Where(goqu.C("application_id").Eq(appID), goqu.C("arch").Eq(arch), goqu.C("version").Eq(version)).
		ToSQL()
	if err != nil {
		return nil, err
	}
	err = q.db.QueryRowx(query).StructScan(&pkg)
	if err != nil {
		return nil, err
	}
	flatcarAction, err := q.getFlatcarAction(pkg.ID)
	switch err {
	case nil:
		pkg.FlatcarAction = flatcarAction
	case sql.ErrNoRows:
		pkg.FlatcarAction = nil
	default:
		return nil, err
	}
	return &pkg, nil
}

func (q *Queries) GetPackagesCount(appID string, searchVersion *string) (int, error) {
	query := goqu.From(goqu.L("package LEFT JOIN package_channel_blacklist pcb ON package.id = pcb.package_id")).
		Select(goqu.L(`package.*,
	    array_agg(pcb.channel_id) FILTER (WHERE pcb.channel_id IS NOT NULL) as channels_blacklist
	    `)).Where(goqu.C("application_id").Eq(appID)).
		GroupBy("package.id")
	if searchVersion != nil {
		sv := "%" + strings.ToLower(*searchVersion) + "%"
		query = query.Where(goqu.I("version").ILike(sv))
	}
	query = goqu.From(query).Select(goqu.L("count (*)"))
	return q.GetCountQuery(query)
}

func (q *Queries) GetPackages(appID string, page, perPage uint64, searchVersion *string) ([]*api.Package, error) {
	page, perPage = shared.ValidatePaginationParams(page, perPage)
	limit, offset := shared.SQLPaginate(page, perPage)
	query := q.packagesQuery().
		Where(goqu.C("application_id").Eq(appID)).
		Limit(limit).
		Offset(offset)
	if searchVersion != nil {
		sv := "%" + strings.ToLower(*searchVersion) + "%"
		query = query.Where(goqu.I("version").ILike(sv))
	}
	queryString, _, err := query.ToSQL()
	if err != nil {
		return nil, err
	}
	return q.getPackagesFromQuery(queryString)
}

func (q *Queries) getPackagesFromQuery(query string) ([]*api.Package, error) {
	var pkgs []*api.Package
	rows, err := q.db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var flatcarAction *api.FlatcarAction
		pkg := api.Package{}
		err = rows.StructScan(&pkg)
		if err != nil {
			return nil, err
		}
		flatcarAction, err = q.getFlatcarAction(pkg.ID)
		switch err {
		case nil:
			pkg.FlatcarAction = flatcarAction
		case sql.ErrNoRows:
			pkg.FlatcarAction = nil
		default:
			return nil, err
		}
		extraFiles, err := q.getExtraFiles(pkg.ID)
		switch err {
		case nil:
			pkg.ExtraFiles = extraFiles
		case sql.ErrNoRows:
			pkg.ExtraFiles = nil
		default:
			return nil, err
		}
		pkgs = append(pkgs, &pkg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return pkgs, nil
}

func (q *Queries) packagesQuery() *goqu.SelectDataset {
	return goqu.From(goqu.L("package LEFT JOIN package_channel_blacklist pcb ON package.id = pcb.package_id")).
		Select(goqu.L(`package.*,
	    array_agg(pcb.channel_id) FILTER (WHERE pcb.channel_id IS NOT NULL) as channels_blacklist
	    `)).
		GroupBy("package.id").Order(goqu.L("regexp_matches(version, '(\\d+)\\.(\\d+)\\.(\\d+)')::int[]").Desc())
}

func (q *Queries) getFlatcarActionQuery(packageID string) *goqu.SelectDataset {
	return goqu.From("flatcar_action").Where(goqu.C("package_id").Eq(packageID))
}

func (q *Queries) getFlatcarAction(packageID string) (*api.FlatcarAction, error) {
	query, _, err := q.getFlatcarActionQuery(packageID).ToSQL()
	if err != nil {
		return nil, err
	}
	flatcarAction := api.FlatcarAction{}
	err = q.db.QueryRowx(query).StructScan(&flatcarAction)
	if err != nil {
		return nil, err
	}
	return &flatcarAction, nil
}

func (q *Queries) getExtraFiles(packageID string) ([]api.File, error) {
	query, _, err := goqu.From("package_file").Where(goqu.C("package_id").Eq(packageID)).Order(goqu.C("id").Asc()).ToSQL()
	if err != nil {
		return nil, err
	}
	var files []api.File
	rows, err := q.db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		f := api.File{}
		if err := rows.StructScan(&f); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return files, nil
}

func (q *Queries) getPackage(packageID null.String) (*api.Package, error) {
	query, _, err := q.packagesQuery().Where(goqu.C("id").Eq(packageID)).ToSQL()
	if err != nil {
		return nil, err
	}
	packageEntity := api.Package{}
	err = q.db.QueryRowx(query).StructScan(&packageEntity)
	if err != nil {
		return nil, err
	}
	flatcarAction, err := q.getFlatcarAction(packageEntity.ID)
	switch err {
	case nil:
		packageEntity.FlatcarAction = flatcarAction
	case sql.ErrNoRows:
		packageEntity.FlatcarAction = nil
	default:
		return nil, err
	}
	extraFiles, err := q.getExtraFiles(packageEntity.ID)
	switch err {
	case nil:
		packageEntity.ExtraFiles = extraFiles
	case sql.ErrNoRows:
		packageEntity.ExtraFiles = nil
	default:
		return nil, err
	}
	return &packageEntity, nil
}
