package http

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
# HELP test_api_request_duration_seconds Duration of API requests in seconds
# TYPE test_api_request_duration_seconds histogram
`
		assert.NoError(t, testutil.CollectAndCompare(m.apiRequestDuration, strings.NewReader(expected)))
	})

	t.Run("counter metrics increment correctly", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")

		// Test apiRequestsTotal
		m.RecordAPIRequest("github", "get_repo")
		assert.Equal(t, float64(1), testutil.ToFloat64(m.apiRequestsTotal.WithLabelValues("github", "get_repo")))

		// Test apiRequestsErrors
		m.RecordAPIError("github", "get_repo", "rate_limit_exceeded")
		assert.Equal(t, float64(1), testutil.ToFloat64(m.apiRequestsErrors.WithLabelValues("github", "get_repo", "rate_limit_exceeded")))
	})

	t.Run("histogram metrics record correctly", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")

		m.ObserveAPIRequestDuration("github", "get_repo", 0.2)
		m.ObserveAPIRequestDuration("github", "get_repo", 0.3)

		expected := `
# HELP test_api_request_duration_seconds Duration of API requests in seconds
# TYPE test_api_request_duration_seconds histogram
test_api_request_duration_seconds_bucket{operation="get_repo",service="github",le="0.05"} 0
test_api_request_duration_seconds_bucket{operation="get_repo",service="github",le="0.1"} 0
test_api_request_duration_seconds_bucket{operation="get_repo",service="github",le="0.25"} 1
test_api_request_duration_seconds_bucket{operation="get_repo",service="github",le="0.5"} 2
test_api_request_duration_seconds_bucket{operation="get_repo",service="github",le="1"} 2
test_api_request_duration_seconds_bucket{operation="get_repo",service="github",le="2.5"} 2
test_api_request_duration_seconds_bucket{operation="get_repo",service="github",le="5"} 2
test_api_request_duration_seconds_bucket{operation="get_repo",service="github",le="10"} 2
test_api_request_duration_seconds_bucket{operation="get_repo",service="github",le="+Inf"} 2
test_api_request_duration_seconds_sum{operation="get_repo",service="github"} 0.5
test_api_request_duration_seconds_count{operation="get_repo",service="github"} 2
`
		assert.NoError(t, testutil.CollectAndCompare(m.apiRequestDuration, strings.NewReader(expected)))
	})
}
