// Package dbreads provides shared read operations used by both
// the admin and runtime sub-packages. The Queries struct is embedded into
// both services, giving them direct read access through their own DB connection.
package dbreads

import (
	"github.com/jmoiron/sqlx"

	"github.com/flatcar/nebraska/backend/pkg/logger"
)

var l = logger.New("dbreads")

// Queries provides all shared read operations.
type Queries struct {
	db *sqlx.DB
}

// New creates a new Queries instance.
func New(db *sqlx.DB) *Queries {
	return &Queries{db: db}
}
