package shared

import (
	"fmt"

	"github.com/blang/semver/v4"
)

const (
	defaultPage    uint64 = 1
	defaultPerPage uint64 = 10
)

// ValidatePaginationParams validates the pagination parameters provided,
// setting them to the default values in case they are invalid.
func ValidatePaginationParams(page, perPage uint64) (uint64, uint64) {
	if page < 1 {
		page = defaultPage
	}
	if perPage < 1 {
		perPage = defaultPerPage
	}
	return page, perPage
}

// SQLPaginate calculates SQL LIMIT and OFFSET from page parameters.
func SQLPaginate(page, perPage uint64) (uint, uint) {
	return uint(perPage), uint(page-1) * uint(perPage)
}

// IgnoreFakeInstanceCondition returns a SQL condition that filters out fake/placeholder instance IDs.
func IgnoreFakeInstanceCondition(field string) string {
	return fmt.Sprintf(`(%[1]s IS NULL OR %[1]s NOT LIKE '{________-____-____-____-____________}')`, field)
}

// IsValidSemver checks if the provided string is a valid semver version.
func IsValidSemver(version string) bool {
	if _, err := semver.Make(version); err != nil {
		return false
	}
	return true
}
