package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRolloutsRepo(t *testing.T) {
	ctx := context.Background()
	helper := newTestHelper(t)
	helper.setup(ctx)
	defer helper.teardown(ctx)

	setupTest(t)
	repo, err := NewRolloutsRepo(ctx, helper.log, helper.cfg, NewMetrics("test"))
	require.NoError(t, err)

	_, err = repo.Get(ctx, "glamsterdam-devnet-12", "guild")
	require.ErrorIs(t, err, ErrRolloutAlertNotFound)

	alert := &RolloutAlert{Network: "glamsterdam-devnet-12", DiscordGuildID: "guild", DiscordChannel: "chan", LastEventID: 41, CreatedAt: time.Now().UTC()}
	require.NoError(t, repo.Persist(ctx, alert))

	got, err := repo.Get(ctx, "glamsterdam-devnet-12", "guild")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(41), got.LastEventID)
	assert.Equal(t, "chan", got.DiscordChannel)

	// Mentions under the same network are not rollout alerts.
	mentions, err := NewMentionsRepo(ctx, helper.log, helper.cfg, NewMetrics("test"))
	require.NoError(t, err)
	require.NoError(t, mentions.Persist(ctx, &ClientMention{Network: "glamsterdam-devnet-12", Client: "lighthouse", DiscordGuildID: "guild"}))

	all, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)

	assert.True(t, strings.HasSuffix(repo.Key(alert), "/networks/glamsterdam-devnet-12/rollouts/guild.json"))
	assert.Empty(t, repo.Key(nil))

	require.Error(t, repo.Purge(ctx, "only-one"))
	require.NoError(t, repo.Purge(ctx, "glamsterdam-devnet-12", "guild"))

	all, err = repo.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, all)
}
