// Package config centralizes the operational defaults that were previously
// scattered as magic literals across main and the background workers.
// Values here are defaults only — most can be overridden at runtime through
// the Settings page; nothing in this package reads configuration itself.
package config

import "time"

const (
	// DefaultCheckInterval is the backend health-check period when the
	// check_interval setting is absent (seconds are configurable live).
	DefaultCheckInterval = 30 * time.Second

	// MinCheckInterval is the fastest health-check round we allow.
	MinCheckInterval = 5 * time.Second

	// DefaultScanInterval is the port-scan period when the scan_interval
	// setting is absent.
	DefaultScanInterval = 5 * time.Minute

	// MinScanInterval is the fastest scan round we allow.
	MinScanInterval = 30 * time.Second

	// SSESampleInterval is how often the system-stats sampler publishes
	// to the SSE bus.
	SSESampleInterval = 5 * time.Second

	// ConntrackSampleInterval is the live connection-log sampling period.
	ConntrackSampleInterval = 5 * time.Second

	// MetricsSampleInterval is the per-node metric point cadence.
	MetricsSampleInterval = 30 * time.Second

	// MetricsRetention is how long metric points are kept before pruning.
	MetricsRetention = 7 * 24 * time.Hour

	// AlertsRoundInterval is the alerter evaluation period.
	AlertsRoundInterval = 60 * time.Second

	// AuditTailRows is the default audit trail length returned by the API.
	AuditTailRows = 300

	// ListMaxLimit caps ?limit= on paginated list endpoints.
	ListMaxLimit = 500
)
