// Package shared provides constants and helpers used by the api sub-packages
// (admin, runtime, dbreads). This package is internal to pkg/api and cannot be
// imported by packages outside that tree.
package shared

// Duration constants used by instance and group queries.
type PostgresDuration string

const (
	ValidityInterval PostgresDuration = "1 days"
)

// Runtime Activity class constants.
const (
	ActivityPackageNotFound int = 1 + iota
	ActivityRolloutStarted
	ActivityRolloutFinished
	ActivityRolloutFailed
	ActivityInstanceUpdateFailed
)

// Activity severity constants.
const (
	ActivitySuccess int = 1 + iota
	ActivityInfo
	ActivityWarning
	ActivityError
)
