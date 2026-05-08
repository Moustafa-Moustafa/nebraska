package dbreads

import (
	"github.com/doug-martin/goqu/v9"

	"github.com/flatcar/nebraska/backend/pkg/api"
)

func (q *Queries) GetFlatcarAction(packageID string) (*api.FlatcarAction, error) {
	action := api.FlatcarAction{}
	query, _, err := goqu.From("flatcar_action").
		Where(goqu.C("package_id").Eq(packageID)).ToSQL()
	if err != nil {
		return nil, err
	}
	err = q.db.QueryRowx(query).StructScan(&action)
	if err != nil {
		return nil, err
	}
	return &action, nil
}
