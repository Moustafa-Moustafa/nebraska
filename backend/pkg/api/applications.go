package api

import (
	"time"

	"gopkg.in/guregu/null.v4"
)

const (
	flatcarAppID = "e96281a6-d1af-4bde-9a0a-97b76e56dc57"

	// FlatcarAppID is the well-known application ID for Flatcar Linux.
	// Exported for use by runtime sub-package.
	FlatcarAppID = flatcarAppID
)

// Application represents a Nebraska application instance.
type Application struct {
	ID          string      `db:"id" json:"id"`
	ProductID   null.String `db:"product_id" json:"product_id"`
	Name        string      `db:"name" json:"name"`
	Description string      `db:"description" json:"description"`
	CreatedTs   time.Time   `db:"created_ts" json:"created_ts"`
	TeamID      string      `db:"team_id" json:"-"`
	Groups      []*Group    `db:"groups" json:"groups"`
	Channels    []*Channel  `db:"channels" json:"channels"`

	Instances struct {
		Count int `db:"count" json:"count"`
	} `db:"instances" json:"instances,omitempty"`
}
