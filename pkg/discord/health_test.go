package discord

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newHealthTestBot(t *testing.T) *DiscordBot {
	t.Helper()

	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	bot, err := NewBot(logrus.New(), &Config{DiscordToken: "test"}, nil, nil, nil, nil, nil, nil, nil, NewMetrics("test"), nil)
	require.NoError(t, err)

	db, ok := bot.(*DiscordBot)
	require.True(t, ok)

	return db
}

func TestHealthy(t *testing.T) {
	t.Run("healthy within grace period before first connect", func(t *testing.T) {
		bot := newHealthTestBot(t)

		assert.NoError(t, bot.Healthy())
		assert.Equal(t, float64(0), testutil.ToFloat64(bot.metrics.gatewayConnected))
	})

	t.Run("unhealthy if never connected past grace period", func(t *testing.T) {
		bot := newHealthTestBot(t)
		bot.disconnectedAt = time.Now().Add(-disconnectGracePeriod - time.Second)

		assert.Error(t, bot.Healthy())
	})

	t.Run("healthy once connected", func(t *testing.T) {
		bot := newHealthTestBot(t)
		bot.disconnectedAt = time.Now().Add(-disconnectGracePeriod - time.Second)

		bot.handleConnect(nil, nil)

		assert.NoError(t, bot.Healthy())
		assert.Equal(t, float64(1), testutil.ToFloat64(bot.metrics.gatewayConnected))
	})

	t.Run("disconnect starts a fresh grace period", func(t *testing.T) {
		bot := newHealthTestBot(t)
		bot.disconnectedAt = time.Now().Add(-disconnectGracePeriod - time.Second)

		bot.handleConnect(nil, nil)
		bot.handleDisconnect(nil, nil)

		assert.NoError(t, bot.Healthy())
		assert.Equal(t, float64(0), testutil.ToFloat64(bot.metrics.gatewayConnected))
		assert.WithinDuration(t, time.Now(), bot.disconnectedAt, time.Second)
	})

	t.Run("unhealthy after prolonged disconnect", func(t *testing.T) {
		bot := newHealthTestBot(t)

		bot.handleConnect(nil, nil)
		bot.handleDisconnect(nil, nil)
		bot.disconnectedAt = time.Now().Add(-disconnectGracePeriod - time.Second)

		err := bot.Healthy()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "discord gateway disconnected for")
	})

	t.Run("repeated disconnects do not extend the grace period", func(t *testing.T) {
		bot := newHealthTestBot(t)

		bot.handleConnect(nil, nil)
		bot.handleDisconnect(nil, nil)
		bot.disconnectedAt = time.Now().Add(-disconnectGracePeriod - time.Second)
		bot.handleDisconnect(nil, nil)

		assert.Error(t, bot.Healthy())
	})
}
