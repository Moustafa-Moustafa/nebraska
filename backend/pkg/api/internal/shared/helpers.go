// Package shared holds constants, type aliases, and small helpers used by
// the api sub-packages (admin, runtime, dbreads). It is internal to pkg/api
// and cannot be imported by packages outside that tree.
package shared

import (
	"fmt"

	"github.com/blang/semver/v4"
)

// PostgresDuration is the Go shape of a Postgres INTERVAL string passed
// directly into queries (e.g. "1 days", "30 minutes").
type PostgresDuration string

// ValidityInterval is the window inside which an instance is considered
// "alive" (i.e. has reported recently enough to be counted in stats and
// rollout policies). Used by application/group/instance read helpers.
const ValidityInterval PostgresDuration = "1 days"

const (
	defaultPage    uint64 = 1
	defaultPerPage uint64 = 10
)

// ValidatePaginationParams clamps the pagination parameters to sane defaults
// when zero/negative values are provided.
func ValidatePaginationParams(page, perPage uint64) (uint64, uint64) {
	if page < 1 {
		page = defaultPage
	}
	if perPage < 1 {
		perPage = defaultPerPage
	}
	return page, perPage
}

// SQLPaginate computes SQL LIMIT and OFFSET from page parameters.
func SQLPaginate(page, perPage uint64) (uint, uint) {
	return uint(perPage), uint(page-1) * uint(perPage)
}

// IgnoreFakeInstanceCondition returns a SQL fragment that filters out
// fake/placeholder instance ids (used by tests). instanceIDField is a column
// name and must be a safe SQL identifier supplied by the code, never by user
// input.
func IgnoreFakeInstanceCondition(instanceIDField string) string {
	return fmt.Sprintf(`(%[1]s IS NULL OR %[1]s NOT LIKE '{________-____-____-____-____________}')`, instanceIDField)
}

// IsValidSemver reports whether the provided string is a valid semver
// version, per github.com/blang/semver.
func IsValidSemver(version string) bool {
	if _, err := semver.Make(version); err != nil {
		return false
	}
	return true
}
