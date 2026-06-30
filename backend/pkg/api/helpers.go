package api

import (
	"time"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

// validatePaginationParams forwards to shared.ValidatePaginationParams. It
// stays here as a thin local name so existing pkg/api callers compile
// unchanged.
var validatePaginationParams = shared.ValidatePaginationParams

// sqlPaginate forwards to shared.SQLPaginate.
var sqlPaginate = shared.SQLPaginate

// isValidSemver forwards to shared.IsValidSemver.
var isValidSemver = shared.IsValidSemver

// isTimezoneValid checks if the provided timezone is valid. Stays local:
// only admin-side group writers use it, no sub-package needs it.
func isTimezoneValid(tz string) bool {
	if tz == "" {
		return false
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return false
	}
	return true
}
