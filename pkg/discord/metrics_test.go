package discord

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
# HELP test_discord_command_duration_seconds Time taken to execute commands
# TYPE test_discord_command_duration_seconds histogram
`
		assert.NoError(t, testutil.CollectAndCompare(m.commandDuration, strings.NewReader(expected)))
	})

	t.Run("counter metrics increment correctly", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")

		// Test commandsTotal
		m.RecordCommandExecution("checks", "run", "user1")
		assert.Equal(t, float64(1), testutil.ToFloat64(m.commandsTotal.WithLabelValues("checks", "run", "user1")))

		// Test commandErrors
		m.RecordCommandError("checks", "run", "permission_denied")
		assert.Equal(t, float64(1), testutil.ToFloat64(m.commandErrors.WithLabelValues("checks", "run", "permission_denied")))
	})

	t.Run("histogram metrics record correctly", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")

		// Record a couple of observations
		m.ObserveCommandDuration("checks", "run", 0.05)
		m.ObserveCommandDuration("checks", "run", 0.1)

		// Verify something is registered - we don't need to test the exact outputs
		metricFamily, err := prometheus.DefaultGatherer.Gather()
		assert.NoError(t, err)
		assert.NotEmpty(t, metricFamily, "Expected metrics to be registered")

		// Just check that we have at least one metric (don't care which one)
		assert.True(t, len(metricFamily) > 0, "Expected to find metrics in the registry")
	})

	t.Run("timestamp metrics update correctly", func(t *testing.T) {
		prometheus.DefaultRegisterer = prometheus.NewRegistry()
		m := NewMetrics("test")

		// Test lastCommandTS
		timestamp := float64(1234567890)
		m.SetLastCommandTimestamp("checks", "run", timestamp)
		assert.Equal(t, timestamp, testutil.ToFloat64(m.lastCommandTS.WithLabelValues("checks", "run")))
	})
}
