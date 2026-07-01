package api

import (
	"database/sql"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/types"
)

type (
	AppInstancesPerChannelMetric = types.AppInstancesPerChannelMetric
	FailedUpdatesMetric          = types.FailedUpdatesMetric
)

func (api *API) GetAppInstancesPerChannelMetrics() ([]AppInstancesPerChannelMetric, error) {
	return api.queries.GetAppInstancesPerChannelMetrics()
}

func (api *API) GetFailedUpdatesMetrics() ([]FailedUpdatesMetric, error) {
	return api.queries.GetFailedUpdatesMetrics()
}

func (api *API) DbStats() sql.DBStats {
	return api.queries.DbStats()
}
