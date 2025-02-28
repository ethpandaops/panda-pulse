package queue

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestMetrics(t *testing.T) {
	t.Run("metrics are registered successfully", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")
		assert.NotNil(t, m)

		expected := `
# HELP test_queue_length_current Current number of checks in queue
# TYPE test_queue_length_current gauge
test_queue_length_current 0
`
		assert.NoError(t, testutil.CollectAndCompare(m.queueLength, strings.NewReader(expected)))
	})

	t.Run("counter metrics increment correctly", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")

		// Test queuedTotal
		m.queuedTotal.WithLabelValues("testnet", "client1").Inc()
		assert.Equal(t, float64(1), testutil.ToFloat64(m.queuedTotal.WithLabelValues("testnet", "client1")))

		// Test processedTotal
		m.processedTotal.WithLabelValues("testnet", "client1", "success").Inc()
		assert.Equal(t, float64(1), testutil.ToFloat64(m.processedTotal.WithLabelValues("testnet", "client1", "success")))

		// Test failuresTotal
		m.failuresTotal.WithLabelValues("testnet", "client1", "worker_error").Inc()
		assert.Equal(t, float64(1), testutil.ToFloat64(m.failuresTotal.WithLabelValues("testnet", "client1", "worker_error")))

		// Test skipsDueToLock
		m.skipsDueToLock.WithLabelValues("testnet", "client1").Inc()
		assert.Equal(t, float64(1), testutil.ToFloat64(m.skipsDueToLock.WithLabelValues("testnet", "client1")))
	})

	t.Run("gauge metrics update correctly", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")

		// Test queueLength
		m.queueLength.Set(5)
		assert.Equal(t, float64(5), testutil.ToFloat64(m.queueLength))

		m.queueLength.Dec()
		assert.Equal(t, float64(4), testutil.ToFloat64(m.queueLength))

		m.queueLength.Inc()
		assert.Equal(t, float64(5), testutil.ToFloat64(m.queueLength))
	})

	t.Run("histogram metrics record correctly", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")

		m.processingTime.WithLabelValues("testnet", "client1").Observe(1.5)
		m.processingTime.WithLabelValues("testnet", "client1").Observe(2.5)

		expected := `
# HELP test_queue_check_processing_duration_seconds Time taken to process checks
# TYPE test_queue_check_processing_duration_seconds histogram
test_queue_check_processing_duration_seconds_bucket{client="client1",network="testnet",le="1"} 0
test_queue_check_processing_duration_seconds_bucket{client="client1",network="testnet",le="5"} 2
test_queue_check_processing_duration_seconds_bucket{client="client1",network="testnet",le="10"} 2
test_queue_check_processing_duration_seconds_bucket{client="client1",network="testnet",le="30"} 2
test_queue_check_processing_duration_seconds_bucket{client="client1",network="testnet",le="60"} 2
test_queue_check_processing_duration_seconds_bucket{client="client1",network="testnet",le="120"} 2
test_queue_check_processing_duration_seconds_bucket{client="client1",network="testnet",le="300"} 2
test_queue_check_processing_duration_seconds_bucket{client="client1",network="testnet",le="+Inf"} 2
test_queue_check_processing_duration_seconds_sum{client="client1",network="testnet"} 4
test_queue_check_processing_duration_seconds_count{client="client1",network="testnet"} 2
`
		assert.NoError(t, testutil.CollectAndCompare(m.processingTime, strings.NewReader(expected)))
	})
}
