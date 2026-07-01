// Package shared holds constants, type aliases, and small helpers used by
// the api sub-packages (admin, runtime, dbreads). It is internal to pkg/api
// and cannot be imported by packages outside that tree.
package shared

import (
	"fmt"

	"github.com/blang/semver/v4"
)

type PostgresDuration string

const ValidityInterval PostgresDuration = "1 days"

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

func SQLPaginate(page, perPage uint64) (uint, uint) {
	return uint(perPage), uint(page-1) * uint(perPage)
}

func IgnoreFakeInstanceCondition(instanceIDField string) string {
	return fmt.Sprintf(`(%[1]s IS NULL OR %[1]s NOT LIKE '{________-____-____-____-____________}')`, instanceIDField)
}

// IsValidSemver checks if the provided string represents a valid semver
// version.
func IsValidSemver(version string) bool {
	if _, err := semver.Make(version); err != nil {
		return false
	}
	return true
}
