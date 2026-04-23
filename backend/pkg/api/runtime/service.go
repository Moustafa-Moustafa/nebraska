// Package runtime provides write operations for runtime tables
// (instance, instance_application, instance_status_history, event,
// activity, group_state). These operations are available on all instances
// (primary and subscriber).
//
// The Service struct receives its own *sqlx.DB connection and a Reader
// interface for read operations. Code in this package cannot access the
// admin sub-package's DB connection, enforcing the admin/runtime
// boundary at compile time.
package runtime

import (
	"github.com/jmoiron/sqlx"

	"github.com/flatcar/nebraska/backend/pkg/api/dbreads"
	"github.com/flatcar/nebraska/backend/pkg/logger"
)

var l = logger.New("runtime")

// Service provides runtime write operations. It embeds dbreads.Queries for
// read access using the same DB connection. Code in this package cannot
// access the admin sub-package's DB connection.
type Service struct {
	dbreads.Queries // embedded — all read methods available
	db              *sqlx.DB

	disableUpdatesOnFailedRollout bool
}

// NewService creates a new runtime Service.
func NewService(db *sqlx.DB, disableUpdatesOnFailedRollout bool) *Service {
	return &Service{
		Queries:                       *dbreads.New(db),
		db:                            db,
		disableUpdatesOnFailedRollout: disableUpdatesOnFailedRollout,
	}
}
