package dbreads

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/doug-martin/goqu/v9"
	"gopkg.in/guregu/null.v4"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/types"
)

// safeIdentifierRE matches column identifiers (and optional table.column form)
// that are safe to interpolate into raw SQL. Used by semverToIntArray to
// guard against SQL injection when callers pass a non-literal column name.
var safeIdentifierRE = regexp.MustCompile(`^[a-z_]+(\.[a-z_]+)?$`)

// semverToIntArray returns a Postgres expression that converts a semver
// version string into an int[] for ordering/comparison.
// The column parameter must be a safe SQL identifier (verified here).
func semverToIntArray(column string) (string, error) {
	if column != "?" && !safeIdentifierRE.MatchString(column) {
		return "", fmt.Errorf("semverToIntArray: invalid column name %q - potential SQL injection", column)
	}
	return fmt.Sprintf("string_to_array((regexp_split_to_array(%s, '[+-]'))[1], '.')::int[]", column), nil
}

// versionCompareExpr returns a goqu expression that compares the given
// column to the given value as a semver-aware int[] comparison. Allowed
// operators: >, >=, <, <=, =, !=.
func versionCompareExpr(column, operator, value string) (goqu.Expression, error) {
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

// GetPackage returns the package identified by the id provided.
func (q *Queries) GetPackage(pkgID string) (*types.Package, error) {
	return q.getPackage(null.StringFrom(pkgID))
}

// GetPackageByVersionAndArch returns the package identified by the
// application ID, version and arch provided.
func (q *Queries) GetPackageByVersionAndArch(appID, version string, arch types.Arch) (*types.Package, error) {
	var pkg types.Package
	if !shared.IsValidSemver(version) {
		return nil, fmt.Errorf("error GetPackageByVersionAndArch version %s is not valid", version)
	}
	query, _, err := q.packagesQuery().
		Where(goqu.C("application_id").Eq(appID), goqu.C("arch").Eq(arch), goqu.C("version").Eq(version)).
		ToSQL()
	if err != nil {
		return nil, err
	}
	if err := q.db.QueryRowx(query).StructScan(&pkg); err != nil {
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

// GetPackagesCount returns the total number of packages in an app, optionally
// filtered by a version search string.
func (q *Queries) GetPackagesCount(appID string, searchVersion *string) (int, error) {
	query := goqu.From(goqu.L("package LEFT JOIN package_channel_blacklist pcb ON package.id = pcb.package_id")).
		Select(goqu.L(`package.*,
	    array_agg(pcb.channel_id) FILTER (WHERE pcb.channel_id IS NOT NULL) as channels_blacklist
	    `)).Where(goqu.C("application_id").Eq(appID)).
		GroupBy("package.id")
	if searchVersion != nil {
		*searchVersion = "%" + strings.ToLower(*searchVersion) + "%"
		query = query.Where(goqu.I("version").ILike(*searchVersion))
	}
	query = goqu.From(query).Select(goqu.L("count (*)"))
	return q.GetCountQuery(query)
}

// GetPackages returns all packages associated to the application provided.
func (q *Queries) GetPackages(appID string, page, perPage uint64, searchVersion *string) ([]*types.Package, error) {
	page, perPage = shared.ValidatePaginationParams(page, perPage)
	limit, offset := shared.SQLPaginate(page, perPage)
	query := q.packagesQuery().
		Where(goqu.C("application_id").Eq(appID)).
		Limit(limit).
		Offset(offset)
	if searchVersion != nil {
		*searchVersion = "%" + strings.ToLower(*searchVersion) + "%"
		query = query.Where(goqu.I("version").ILike(*searchVersion))
	}
	queryString, _, err := query.ToSQL()
	if err != nil {
		return nil, err
	}
	return q.getPackagesFromQuery(queryString)
}

func (q *Queries) getPackagesFromQuery(query string) ([]*types.Package, error) {
	var pkgs []*types.Package
	rows, err := q.db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		pkg := types.Package{}
		if err := rows.StructScan(&pkg); err != nil {
			return nil, err
		}
		pkgs = append(pkgs, &pkg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return pkgs, nil
	}
	return q.loadPackageExtras(pkgs)
}

// loadPackageExtras loads extra files and flatcar actions for a slice of
// packages efficiently (one query per kind, instead of one per package).
func (q *Queries) loadPackageExtras(packages []*types.Package) ([]*types.Package, error) {
	if len(packages) == 0 {
		return packages, nil
	}
	pkgIDs := make([]string, len(packages))
	for i, pkg := range packages {
		pkgIDs[i] = pkg.ID
	}

	query, _, err := goqu.From("package_file").
		Where(goqu.C("package_id").In(pkgIDs)).
		Order(goqu.C("package_id").Asc(), goqu.C("id").Asc()).
		ToSQL()
	if err != nil {
		return nil, err
	}
	var files []types.File
	if err := q.db.Select(&files, query); err != nil {
		return nil, err
	}
	filesByPkg := make(map[string][]types.File)
	for _, file := range files {
		filesByPkg[file.PackageID] = append(filesByPkg[file.PackageID], file)
	}

	query, _, err = goqu.From("flatcar_action").
		Where(goqu.C("package_id").In(pkgIDs)).
		ToSQL()
	if err != nil {
		return nil, err
	}
	var actions []types.FlatcarAction
	if err := q.db.Select(&actions, query); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	actionsByPkg := make(map[string]*types.FlatcarAction)
	for i := range actions {
		actionsByPkg[actions[i].PackageID] = &actions[i]
	}

	for _, pkg := range packages {
		if files, ok := filesByPkg[pkg.ID]; ok {
			pkg.ExtraFiles = files
		} else {
			pkg.ExtraFiles = nil
		}
		if action, ok := actionsByPkg[pkg.ID]; ok {
			pkg.FlatcarAction = action
		} else {
			pkg.FlatcarAction = nil
		}
	}
	return packages, nil
}

// packagesQuery returns a SelectDataset prepared to return all packages,
// ordered by semver descending. Callers extend the base with additional
// filters.
func (q *Queries) packagesQuery() *goqu.SelectDataset {
	// Note: semverToIntArray error handling is deferred to when ToSQL() is
	// called since goqu.SelectDataset doesn't support immediate error returns.
	semverExpr, err := semverToIntArray("version")
	if err != nil {
		return goqu.From("invalid_table_error_" + err.Error())
	}
	return goqu.From(goqu.L("package LEFT JOIN package_channel_blacklist pcb ON package.id = pcb.package_id")).
		Select(goqu.L(`package.*,
	    array_agg(pcb.channel_id) FILTER (WHERE pcb.channel_id IS NOT NULL) as channels_blacklist
	    `)).
		GroupBy("package.id").Order(goqu.L(semverExpr).Desc())
}

func (q *Queries) getFlatcarActionQuery(packageID string) *goqu.SelectDataset {
	return goqu.From("flatcar_action").Where(goqu.C("package_id").Eq(packageID))
}

func (q *Queries) getFlatcarAction(packageID string) (*types.FlatcarAction, error) {
	query, _, err := q.getFlatcarActionQuery(packageID).ToSQL()
	if err != nil {
		return nil, err
	}
	flatcarAction := types.FlatcarAction{}
	if err := q.db.QueryRowx(query).StructScan(&flatcarAction); err != nil {
		return nil, err
	}
	return &flatcarAction, nil
}

func (q *Queries) getExtraFiles(packageID string) ([]types.File, error) {
	query, _, err := goqu.From("package_file").
		Where(goqu.C("package_id").Eq(packageID)).
		Order(goqu.C("id").Asc()).
		ToSQL()
	if err != nil {
		return nil, err
	}
	var files []types.File
	rows, err := q.db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		f := types.File{}
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

// getPackage loads a single package by its null-wrapped id and hydrates the
// FlatcarAction and ExtraFiles fields. Called from GetPackage and from
// channel reads that resolve a channel's current package.
func (q *Queries) getPackage(packageID null.String) (*types.Package, error) {
	query, _, err := q.packagesQuery().Where(goqu.C("id").Eq(packageID)).ToSQL()
	if err != nil {
		return nil, err
	}
	packageEntity := types.Package{}
	if err := q.db.QueryRowx(query).StructScan(&packageEntity); err != nil {
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
