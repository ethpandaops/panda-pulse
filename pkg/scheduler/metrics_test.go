package scheduler

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestMetrics(t *testing.T) {
	// Reset the default registry to avoid conflicts
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	t.Run("metrics are registered successfully", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")
		assert.NotNil(t, m)

		expected := `
# HELP test_scheduler_active_jobs Current number of active jobs
# TYPE test_scheduler_active_jobs gauge
test_scheduler_active_jobs 0
`
		assert.NoError(t, testutil.CollectAndCompare(m.activeJobs, strings.NewReader(expected)))
	})

	t.Run("counter metrics increment correctly", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")

		// Test jobsTotal
		m.jobsTotal.WithLabelValues("* * * * *").Inc()
		assert.Equal(t, float64(1), testutil.ToFloat64(m.jobsTotal.WithLabelValues("* * * * *")))

		// Test jobExecutions
		m.jobExecutions.WithLabelValues("test_job", "* * * * *").Inc()
		assert.Equal(t, float64(1), testutil.ToFloat64(m.jobExecutions.WithLabelValues("test_job", "* * * * *")))

		// Test jobFailures
		m.jobFailures.WithLabelValues("test_job", "* * * * *").Inc()
		assert.Equal(t, float64(1), testutil.ToFloat64(m.jobFailures.WithLabelValues("test_job", "* * * * *")))
	})

	t.Run("gauge metrics update correctly", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")

		// Test activeJobs
		m.activeJobs.Set(3)
		assert.Equal(t, float64(3), testutil.ToFloat64(m.activeJobs))

		m.activeJobs.Dec()
		assert.Equal(t, float64(2), testutil.ToFloat64(m.activeJobs))

		m.activeJobs.Inc()
		assert.Equal(t, float64(3), testutil.ToFloat64(m.activeJobs))
	})

	t.Run("histogram metrics record correctly", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")

		m.executionTime.WithLabelValues("test_job").Observe(0.5)
		m.executionTime.WithLabelValues("test_job").Observe(1.5)

		expected := `
# HELP test_scheduler_job_execution_duration_seconds Time taken to execute jobs
# TYPE test_scheduler_job_execution_duration_seconds histogram
test_scheduler_job_execution_duration_seconds_bucket{name="test_job",le="0.1"} 0
test_scheduler_job_execution_duration_seconds_bucket{name="test_job",le="0.5"} 1
test_scheduler_job_execution_duration_seconds_bucket{name="test_job",le="1"} 1
test_scheduler_job_execution_duration_seconds_bucket{name="test_job",le="2"} 2
test_scheduler_job_execution_duration_seconds_bucket{name="test_job",le="5"} 2
test_scheduler_job_execution_duration_seconds_bucket{name="test_job",le="10"} 2
test_scheduler_job_execution_duration_seconds_bucket{name="test_job",le="30"} 2
test_scheduler_job_execution_duration_seconds_bucket{name="test_job",le="+Inf"} 2
test_scheduler_job_execution_duration_seconds_sum{name="test_job"} 2
test_scheduler_job_execution_duration_seconds_count{name="test_job"} 2
`
		assert.NoError(t, testutil.CollectAndCompare(m.executionTime, strings.NewReader(expected)))
	})

	t.Run("timestamp metrics update correctly", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")

		// Test lastExecutionTS
		timestamp := float64(1234567890)
		m.lastExecutionTS.WithLabelValues("test_job", "* * * * *").Set(timestamp)
		assert.Equal(t, timestamp, testutil.ToFloat64(m.lastExecutionTS.WithLabelValues("test_job", "* * * * *")))
	})
}
