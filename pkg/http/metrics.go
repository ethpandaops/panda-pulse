package http

import "github.com/prometheus/client_golang/prometheus"

// Metrics for API connections.
type Metrics struct {
	apiRequestsTotal   *prometheus.CounterVec
	apiRequestsErrors  *prometheus.CounterVec
	apiRequestDuration *prometheus.HistogramVec
}

// NewMetrics creates a new API metrics instance.
func NewMetrics(namespace string) *Metrics {
	m := &Metrics{
		apiRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "api",
			Name:      "requests_total",
			Help:      "Total number of API requests made",
		}, []string{"service", "operation"}),

		apiRequestsErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "api",
			Name:      "request_errors_total",
			Help:      "Total number of API request errors",
		}, []string{"service", "operation", "error_type"}),

		apiRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "api",
			Name:      "request_duration_seconds",
			Help:      "Duration of API requests in seconds",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"service", "operation"}),
	}

	prometheus.MustRegister(
		m.apiRequestsTotal,
		m.apiRequestsErrors,
		m.apiRequestDuration,
	)

	return m
}

// RecordAPIRequest increments the API request counter.
func (m *Metrics) RecordAPIRequest(service, operation string) {
	m.apiRequestsTotal.WithLabelValues(service, operation).Inc()
}

// RecordAPIError increments the API error counter.
func (m *Metrics) RecordAPIError(service, operation, errorType string) {
	m.apiRequestsErrors.WithLabelValues(service, operation, errorType).Inc()
}

// ObserveAPIRequestDuration records the duration of an API request.
func (m *Metrics) ObserveAPIRequestDuration(service, operation string, duration float64) {
	m.apiRequestDuration.WithLabelValues(service, operation).Observe(duration)
}
