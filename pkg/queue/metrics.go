package queue

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	queuedTotal    *prometheus.CounterVec
	processedTotal *prometheus.CounterVec
	failuresTotal  *prometheus.CounterVec
	queueLength    prometheus.Gauge
	processingTime *prometheus.HistogramVec
	skipsDueToLock *prometheus.CounterVec
}

func NewMetrics(namespace string) *Metrics {
	m := &Metrics{
		queuedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "queue",
			Name:      "checks_queued_total",
			Help:      "Total number of checks queued",
		}, []string{"network", "client"}),

		processedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "queue",
			Name:      "checks_processed_total",
			Help:      "Total number of checks processed",
		}, []string{"network", "client", "status"}),

		failuresTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "queue",
			Name:      "checks_failures_total",
			Help:      "Total number of check failures",
		}, []string{"network", "client", "error_type"}),

		queueLength: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "queue",
			Name:      "length_current",
			Help:      "Current number of checks in queue",
		}),

		processingTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "queue",
			Name:      "check_processing_duration_seconds",
			Help:      "Time taken to process checks",
			Buckets:   []float64{1, 5, 10, 30, 60, 120, 300},
		}, []string{"network", "client"}),

		skipsDueToLock: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "queue",
			Name:      "checks_skipped_total",
			Help:      "Number of checks skipped due to lock",
		}, []string{"network", "client"}),
	}

	prometheus.MustRegister(
		m.queuedTotal,
		m.processedTotal,
		m.failuresTotal,
		m.queueLength,
		m.processingTime,
		m.skipsDueToLock,
	)

	return m
}
