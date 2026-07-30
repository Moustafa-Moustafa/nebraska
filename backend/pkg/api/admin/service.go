// Package admin provides write operations for admin-managed tables
// (application, channel, package, groups, team, users).
package admin

import (
	"github.com/jmoiron/sqlx"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/dbconn"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/dbreads"
	"github.com/flatcar/nebraska/backend/pkg/logger"
)

var l = logger.New("admin")

// Service provides admin write operations. It embeds the api's shared
// dbreads.Queries for read access using the same DB connection.
type Service struct {
	*dbreads.Queries
	db *sqlx.DB
}

// NewService creates a new admin Service. It reuses the shared read queries for
// reads and the shared connection for writes, both owned by the api instance
// and passed in by the caller.
func NewService(conn *dbconn.Conn, reads *dbreads.Queries) *Service {
	return &Service{
		Queries: reads,
		db:      dbconn.DB(conn),
	}
}
