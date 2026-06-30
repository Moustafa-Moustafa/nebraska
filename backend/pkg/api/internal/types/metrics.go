package types

// AppInstancesPerChannelMetric reports the number of instances of an
// application/version/channel across the fleet.
type AppInstancesPerChannelMetric struct {
	ApplicationName string `db:"app_name" json:"app_name"`
	Version         string `db:"version" json:"version"`
	ChannelName     string `db:"channel_name" json:"channel_name"`
	InstancesCount  int    `db:"instances_count" json:"instances_count"`
}

// FailedUpdatesMetric reports the number of failed updates per application.
type FailedUpdatesMetric struct {
	ApplicationName string `db:"app_name" json:"app_name"`
	FailureCount    int    `db:"fail_count" json:"fail_count"`
}
