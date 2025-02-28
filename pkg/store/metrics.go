package store

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	operationsTotal   *prometheus.CounterVec
	operationErrors   *prometheus.CounterVec
	operationDuration *prometheus.HistogramVec
	objectsTotal      *prometheus.GaugeVec
	objectSizeBytes   *prometheus.HistogramVec
}

func NewMetrics(namespace string) *Metrics {
	m := &Metrics{
		operationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "store",
			Name:      "operations_total",
			Help:      "Total number of S3 operations performed",
		}, []string{"operation", "repository"}),

		operationErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "store",
			Name:      "operation_errors_total",
			Help:      "Total number of S3 operation errors",
		}, []string{"operation", "repository", "error_type"}),

		operationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "store",
			Name:      "operation_duration_seconds",
			Help:      "Time taken to perform S3 operations",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10},
		}, []string{"operation", "repository"}),

		objectsTotal: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "store",
			Name:      "objects_total",
			Help:      "Total number of objects in storage",
		}, []string{"repository"}),

		objectSizeBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "store",
			Name:      "object_size_bytes",
			Help:      "Size of objects in storage",
			Buckets:   []float64{1024, 10 * 1024, 100 * 1024, 1024 * 1024, 10 * 1024 * 1024},
		}, []string{"repository"}),
	}

	prometheus.MustRegister(
		m.operationsTotal,
		m.operationErrors,
		m.operationDuration,
		m.objectsTotal,
		m.objectSizeBytes,
	)

	return m
}
