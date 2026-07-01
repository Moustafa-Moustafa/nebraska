package api

import (
	"github.com/doug-martin/goqu/v9"
)

func (api *API) GetCountQuery(query *goqu.SelectDataset) (int, error) {
	return api.queries.GetCountQuery(query)
}
