package api

import (
	"github.com/doug-martin/goqu/v9"
)

// GetCountQuery forwards to api.queries; SQL in pkg/api/dbreads/count.go.
func (api *API) GetCountQuery(query *goqu.SelectDataset) (int, error) {
	return api.queries.GetCountQuery(query)
}
