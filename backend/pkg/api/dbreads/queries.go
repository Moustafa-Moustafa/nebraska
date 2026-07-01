// Package dbreads holds the shared read queries used by the api stack.
package dbreads

import (
	"errors"

	"github.com/jmoiron/sqlx"

	"github.com/flatcar/nebraska/backend/pkg/logger"
)

var l = logger.New("dbreads")

// ErrNoPackageFound indicates that the group doesn't have a channel
// assigned or that the channel doesn't have a package assigned.
var ErrNoPackageFound = errors.New("nebraska: no package found")

// DefaultMaxFloorsPerResponse is the default maximum number of floor versions
// to return in a single update response. This limit prevents:
// - Timeouts during sequential syncer updates
// - Very large Omaha responses
// - Overwhelming syncers with too many intermediate versions
// Can be overridden via NEBRASKA_MAX_FLOORS_PER_RESPONSE env var
const DefaultMaxFloorsPerResponse = 5

type Queries struct {
	db                   *sqlx.DB
	maxFloorsPerResponse int
}

func New(db *sqlx.DB, maxFloorsPerResponse int) *Queries {
	if maxFloorsPerResponse <= 0 {
		maxFloorsPerResponse = DefaultMaxFloorsPerResponse
	}
	return &Queries{db: db, maxFloorsPerResponse: maxFloorsPerResponse}
}
