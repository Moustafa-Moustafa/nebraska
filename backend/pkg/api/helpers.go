package api

import (
	"time"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

// isValidSemver checks if the provided string represents a valid semver
// version.
var isValidSemver = shared.IsValidSemver

// isTimezoneValid checks if the provided timezone is valid.
func isTimezoneValid(tz string) bool {
	if tz == "" {
		return false
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return false
	}
	return true
}
