package dbreads

import (
	"time"

	"github.com/doug-martin/goqu/v9"
	"gopkg.in/guregu/null.v4"
)

// GetEvent returns the most recent event row's error_code (if any) for the
// given instance/app at or before the given timestamp. Used when an instance
// reports a failed update and the handler needs the last error context.
func (q *Queries) GetEvent(instanceID string, appID string, timestamp time.Time) (null.String, error) {
	query, _, err := goqu.From("event").
		Select("error_code").
		Where(goqu.C("instance_id").Eq(instanceID)).
		Where(goqu.C("application_id").Eq(appID)).
		Where(goqu.C("created_ts").Lte(timestamp)).
		Order(goqu.C("created_ts").Desc()).
		Limit(1).
		ToSQL()
	if err != nil {
		return null.NewString("", true), err
	}
	var errCode null.String
	if err := q.db.QueryRow(query).Scan(&errCode); err != nil {
		return null.NewString("", true), err
	}
	return errCode, nil
}
