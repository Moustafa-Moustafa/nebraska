package api

import (
	"errors"
	"time"
)

const (
	// Realm used for basic authentication.
	Realm = "nebraska"
)

var (
	// ErrUpdatingPassword indicates that something went wrong while updating
	// the user's password.
	ErrUpdatingPassword = errors.New("nebraska: error updating password")
)

// User represents a Nebraska user.
type User struct {
	ID        string    `db:"id" json:"id"`
	Username  string    `db:"username" json:"username"`
	Secret    string    `db:"secret" json:"secret"`
	CreatedTs time.Time `db:"created_ts" json:"-"`
	TeamID    string    `db:"team_id" json:"team_id"`
}
