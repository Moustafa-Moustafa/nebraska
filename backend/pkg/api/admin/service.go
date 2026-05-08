// Package admin provides write operations for admin-managed tables
// (application, channel, package, groups, team, users). These operations
// are only available on primary instances.
//
// The Service struct receives its own *sqlx.DB connection and a Reader
// interface for read operations. Code in this package cannot access the
// runtime sub-package's DB connection, enforcing the admin/runtime
// boundary at compile time.
package admin

import (
	"github.com/jmoiron/sqlx"

	"github.com/flatcar/nebraska/backend/pkg/api/dbreads"
	"github.com/flatcar/nebraska/backend/pkg/logger"
)

var l = logger.New("admin")

// Service provides admin write operations. It embeds dbreads.Queries for
// read access using the same DB connection. Code in this package cannot
// access the runtime sub-package's DB connection.
type Service struct {
	dbreads.Queries // embedded — all read methods available
	db              *sqlx.DB
}

// NewService creates a new admin Service.
func NewService(db *sqlx.DB) *Service {
	return &Service{
		Queries: *dbreads.New(db),
		db:      db,
	}
}
