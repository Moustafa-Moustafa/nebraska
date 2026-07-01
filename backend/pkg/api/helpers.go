package api

import (
	"time"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

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
