// Package dbconn carries the shared database connection handle used across the
// api stack. It is a low-level package: the read layer (dbreads) and the write
// services (admin, and later runtime) all depend on it for the connection,
// while it depends on nothing else in the api tree. The api package opens the
// handle and owns its lifecycle (pool tuning, migrations, Close); dbconn only
// carries it so the read and write layers share the one connection.
package dbconn

import "github.com/jmoiron/sqlx"

// Conn carries the shared database handle. It is intentionally opaque: the
// handle is reached only through the package-level DB function.
type Conn struct {
	db *sqlx.DB
}

// New wraps an already-opened handle whose lifecycle is owned by the api
// package.
func New(db *sqlx.DB) *Conn {
	return &Conn{db: db}
}

// DB returns the underlying handle. It is a package function rather than a
// method so that api.Conn() can return a *Conn to callers outside this package
// without also handing them the raw handle: dbconn.DB is reachable only from
// within pkg/api, because dbconn is an internal package.
func DB(c *Conn) *sqlx.DB {
	return c.db
}
