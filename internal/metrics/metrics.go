package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_http_requests_total",
			Help: "Total number of HTTP requests processed, labeled by method, path, and status code.",
		},
		[]string{"method", "path", "status"},
	)

	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gateway_http_request_duration_seconds",
			Help:    "Histogram of HTTP request latencies in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	AnalyzerMatchesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_analyzer_policy_matches_total",
			Help: "Total number of policy matches detected by severity.",
		},
		[]string{"severity"},
	)

	AuditQueueLength = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gateway_audit_queue_length",
			Help: "Current number of audit log entries queued in Redis for persistence.",
		},
	)
)

// Register registers all application metrics with the default Prometheus registry.
func Register() {
	prometheus.MustRegister(HTTPRequestsTotal)
	prometheus.MustRegister(HTTPRequestDuration)
	prometheus.MustRegister(AnalyzerMatchesTotal)
	prometheus.MustRegister(AuditQueueLength)
}
