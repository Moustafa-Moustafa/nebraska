package api

import (
	"database/sql"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/types"
)

// AppInstancesPerChannelMetric and FailedUpdatesMetric are owned by
// pkg/api/internal/types; re-exported here.
type (
	AppInstancesPerChannelMetric = types.AppInstancesPerChannelMetric
	FailedUpdatesMetric          = types.FailedUpdatesMetric
)

// GetAppInstancesPerChannelMetrics forwards to api.queries.
func (api *API) GetAppInstancesPerChannelMetrics() ([]AppInstancesPerChannelMetric, error) {
	return api.queries.GetAppInstancesPerChannelMetrics()
}

// GetFailedUpdatesMetrics forwards to api.queries.
func (api *API) GetFailedUpdatesMetrics() ([]FailedUpdatesMetric, error) {
	return api.queries.GetFailedUpdatesMetrics()
}

// DbStats forwards to api.queries.
func (api *API) DbStats() sql.DBStats {
	return api.queries.DbStats()
}
