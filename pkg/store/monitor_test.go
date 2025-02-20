package store

import (
	"context"
	"testing"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonitorRepo(t *testing.T) {
	ctx := context.Background()
	helper := newTestHelper(t)
	helper.setup(ctx)
	defer helper.teardown(ctx)

	t.Run("NewMonitorRepo", func(t *testing.T) {
		repo, err := NewMonitorRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)
		require.NotNil(t, repo)
	})

	t.Run("List_Empty", func(t *testing.T) {
		repo, err := NewMonitorRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		alerts, err := repo.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, alerts)
	})

	t.Run("Persist_And_List", func(t *testing.T) {
		repo, err := NewMonitorRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		alert := &MonitorAlert{
			Network:        "test-net",
			Client:         "test-client",
			CheckID:        "test-check",
			Enabled:        true,
			DiscordChannel: "test-channel",
			Interval:       time.Hour,
			ClientType:     clients.ClientType("test-type"),
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}

		err = repo.Persist(ctx, alert)
		require.NoError(t, err)

		alerts, err := repo.List(ctx)
		require.NoError(t, err)
		require.Len(t, alerts, 1)

		assert.Equal(t, alert.Network, alerts[0].Network)
		assert.Equal(t, alert.Client, alerts[0].Client)
		assert.Equal(t, alert.CheckID, alerts[0].CheckID)
		assert.Equal(t, alert.Enabled, alerts[0].Enabled)
		assert.Equal(t, alert.DiscordChannel, alerts[0].DiscordChannel)
		assert.Equal(t, alert.Interval, alerts[0].Interval)
		assert.Equal(t, alert.ClientType, alerts[0].ClientType)
	})

	t.Run("Purge", func(t *testing.T) {
		repo, err := NewMonitorRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		alert := &MonitorAlert{
			Network: "test-net",
			Client:  "test-client",
		}

		err = repo.Persist(ctx, alert)
		require.NoError(t, err)

		err = repo.Purge(ctx, alert.Network, alert.Client)
		require.NoError(t, err)

		alerts, err := repo.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, alerts)
	})

	t.Run("Purge_Invalid_Identifiers", func(t *testing.T) {
		repo, err := NewMonitorRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		err = repo.Purge(ctx, "test-net") // Missing client identifier
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected network and client identifiers")
	})

	t.Run("Key_Generation", func(t *testing.T) {
		repo, err := NewMonitorRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		alert := &MonitorAlert{
			Network: "test-net",
			Client:  "test-client",
		}

		key := repo.Key(alert)
		assert.Equal(t, "test/networks/test-net/monitor/test-client.json", key)
	})

	t.Run("Key_Nil_Alert", func(t *testing.T) {
		repo, err := NewMonitorRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		key := repo.Key(nil)
		assert.Empty(t, key)
	})
}
