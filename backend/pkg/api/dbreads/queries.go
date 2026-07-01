// Package dbreads holds the shared read queries used by the api stack.
// Reads are pure SELECTs returning types from pkg/api/internal/types. A
// single *Queries instance is constructed by pkg/api.New and embedded into
// *api.API; later PRs will also embed it into *admin.Service and
// *runtime.Service so admin/runtime writers can read internally without
// going through the wrapper.
package dbreads

import (
	"errors"

	"github.com/jmoiron/sqlx"

	"github.com/flatcar/nebraska/backend/pkg/logger"
)

var l = logger.New("dbreads")

// ErrNoPackageFound mirrors pkg/api.ErrNoPackageFound so reads that resolve
// a target package can return it directly. pkg/api re-exports this sentinel
// (var assignment preserves identity).
var ErrNoPackageFound = errors.New("nebraska: no package found")

// DefaultMaxFloorsPerResponse is the default maximum number of floor versions
// to return in a single update response. Mirrors the constant previously
// defined in pkg/api/packages_floors.go.
const DefaultMaxFloorsPerResponse = 5

// Queries provides all shared read operations.
type Queries struct {
	db                   *sqlx.DB
	maxFloorsPerResponse int
}

// New creates a new Queries instance backed by the given database handle.
// maxFloorsPerResponse is the limit applied to GetRequiredChannelFloors;
// non-positive values fall back to DefaultMaxFloorsPerResponse.
func New(db *sqlx.DB, maxFloorsPerResponse int) *Queries {
	if maxFloorsPerResponse <= 0 {
		maxFloorsPerResponse = DefaultMaxFloorsPerResponse
	}
	return &Queries{db: db, maxFloorsPerResponse: maxFloorsPerResponse}
}
