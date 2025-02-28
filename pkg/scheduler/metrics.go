package scheduler

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	jobsTotal       *prometheus.CounterVec
	jobExecutions   *prometheus.CounterVec
	jobFailures     *prometheus.CounterVec
	activeJobs      prometheus.Gauge
	executionTime   *prometheus.HistogramVec
	lastExecutionTS *prometheus.GaugeVec
}

func NewMetrics(namespace string) *Metrics {
	m := &Metrics{
		jobsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "scheduler",
			Name:      "jobs_total",
			Help:      "Total number of jobs registered",
		}, []string{"schedule"}),

		jobExecutions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "scheduler",
			Name:      "job_executions_total",
			Help:      "Total number of job executions",
		}, []string{"name", "schedule"}),

		jobFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "scheduler",
			Name:      "job_failures_total",
			Help:      "Total number of job failures",
		}, []string{"name", "schedule"}),

		activeJobs: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "scheduler",
			Name:      "active_jobs",
			Help:      "Current number of active jobs",
		}),

		executionTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "scheduler",
			Name:      "job_execution_duration_seconds",
			Help:      "Time taken to execute jobs",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30},
		}, []string{"name"}),

		lastExecutionTS: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "scheduler",
			Name:      "job_last_execution_timestamp",
			Help:      "Timestamp of last job execution",
		}, []string{"name", "schedule"}),
	}

	prometheus.MustRegister(
		m.jobsTotal,
		m.jobExecutions,
		m.jobFailures,
		m.activeJobs,
		m.executionTime,
		m.lastExecutionTS,
	)

	return m
}
