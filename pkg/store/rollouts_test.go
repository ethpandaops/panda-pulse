package store

import (
	"context"
	"encoding/json"
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
	metrics := NewMetrics("test")
	repo, err := NewRolloutsRepo(ctx, helper.log, helper.cfg, metrics)
	require.NoError(t, err)

	_, err = repo.Get(ctx, "glamsterdam-devnet-12", "guild")
	require.ErrorIs(t, err, ErrRolloutAlertNotFound)

	alert := &RolloutAlert{Network: "glamsterdam-devnet-12", DiscordGuildID: "guild", DiscordChannel: "chan", CreatedAt: time.Now().UTC()}
	for _, last := range []*int64{new(int64(41)), nil, new(int64(0))} {
		alert.LastEventID = last
		require.NoError(t, repo.Persist(ctx, alert))

		got, getErr := repo.Get(ctx, "glamsterdam-devnet-12", "guild")
		require.NoError(t, getErr)
		require.NotNil(t, got)
		assert.Equal(t, last, got.LastEventID)
		assert.Equal(t, "chan", got.DiscordChannel)
	}

	// Mentions under the same network are not rollout alerts.
	mentions, err := NewMentionsRepo(ctx, helper.log, helper.cfg, metrics)
	require.NoError(t, err)
	require.NoError(t, mentions.Persist(ctx, &ClientMention{Network: "glamsterdam-devnet-12", Client: "lighthouse", DiscordGuildID: "guild"}))

	all, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, new(int64(0)), all[0].LastEventID)

	assert.True(t, strings.HasSuffix(repo.Key(alert), "/networks/glamsterdam-devnet-12/rollouts/guild.json"))
	assert.Empty(t, repo.Key(nil))

	require.Error(t, repo.Purge(ctx, "only-one"))
	require.NoError(t, repo.Purge(ctx, "glamsterdam-devnet-12", "guild"))

	all, err = repo.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestRolloutCursorPresence(t *testing.T) {
	cases := []struct {
		name string
		data string
		want *int64
	}{
		{"missing", `{}`, nil},
		{"null", `{"lastEventId":null}`, nil},
		{"empty history", `{"lastEventId":0}`, new(int64(0))},
		{"existing cursor", `{"lastEventId":41}`, new(int64(41))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var alert RolloutAlert
			require.NoError(t, json.Unmarshal([]byte(tc.data), &alert))
			assert.Equal(t, tc.want, alert.LastEventID)

			data, err := json.Marshal(&alert)
			require.NoError(t, err)

			var restored RolloutAlert
			require.NoError(t, json.Unmarshal(data, &restored))
			assert.Equal(t, tc.want, restored.LastEventID)
		})
	}
}
