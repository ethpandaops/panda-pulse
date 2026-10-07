package rollouts

import (
	"context"
	"fmt"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/panda-pulse/pkg/rolloor"
	"github.com/ethpandaops/panda-pulse/pkg/store"
)

func postedReasons(post *fakePoster) []string {
	reasons := make([]string, 0, len(post.sent))
	for _, msg := range post.sent {
		reasons = append(reasons, msg.Embeds[0].Description)
	}

	return reasons
}

func TestRegisterAgainstEmptyHistoryFollowsFirstEvent(t *testing.T) {
	c, _, hist, post := newTestCommand()
	ctx := context.Background()
	hist.events = nil

	c.register(ctx, "g", net, &discordgo.Channel{ID: "chan"})
	c.poll(ctx)
	hist.events = []rolloor.Event{{ID: 1, Action: "rollout.created", Reason: "first rollout"}}
	c.poll(ctx)
	c.poll(ctx)

	assert.Equal(t, []string{"first rollout"}, postedReasons(post))
}

func TestPollInitialisesEmptyHistoryOnce(t *testing.T) {
	c, alerts, hist, post := newTestCommand()
	ctx := context.Background()
	hist.events = nil
	require.NoError(t, alerts.Persist(ctx, &store.RolloutAlert{Network: net, DiscordGuildID: "g", DiscordChannel: "chan"}))

	c.poll(ctx)
	saved, err := alerts.Get(ctx, net, "g")
	require.NoError(t, err)
	assert.Equal(t, new(int64(0)), saved.LastEventID)

	hist.events = []rolloor.Event{{ID: 1, Action: "rollout.created", Reason: "first rollout"}}
	c.poll(ctx)
	assert.Equal(t, []string{"first rollout"}, postedReasons(post))
}

func TestPollRecoversResetHistory(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty=%t", empty), func(t *testing.T) {
			c, _, hist, post := newTestCommand()
			ctx := context.Background()
			hist.events = []rolloor.Event{{ID: 5000, Action: "rollout.complete", Reason: "old history"}}
			c.register(ctx, "g", net, &discordgo.Channel{ID: "chan"})
			c.poll(ctx)

			if empty {
				hist.events = nil
				c.poll(ctx)
			}

			hist.events = []rolloor.Event{
				{ID: 1, Action: "rollout.created", Reason: "new rollout"},
				{ID: 2, Action: "rollout.paused", Reason: "new pause"},
			}
			c.poll(ctx)
			c.poll(ctx)
			hist.events = append(hist.events, rolloor.Event{ID: 3, Action: "rollout.complete", Reason: "new completion"})
			c.poll(ctx)

			assert.Equal(t, []string{"new rollout", "new pause", "new completion"}, postedReasons(post))
		})
	}
}

func TestPollRetriesInOrderAcrossPages(t *testing.T) {
	for _, failAt := range []int{1, pageSize} {
		t.Run(fmt.Sprintf("failure=%d", failAt), func(t *testing.T) {
			c, _, hist, post := newTestCommand()
			ctx := context.Background()
			hist.events = nil
			c.register(ctx, "g", net, &discordgo.Channel{ID: "chan"})

			want := make([]string, 0, pageSize+3)
			for id := int64(1); id <= pageSize+3; id++ {
				reason := fmt.Sprintf("event %d", id)
				hist.events = append(hist.events, rolloor.Event{ID: id, Action: "rollout.created", Reason: reason})
				want = append(want, reason)
			}

			post.failAt = failAt
			c.poll(ctx)
			assert.Equal(t, want[:failAt-1], postedReasons(post))
			c.poll(ctx)
			assert.Equal(t, want[:failAt-1], postedReasons(post), "later events must wait for the failed event")

			post.failAt = 0
			c.poll(ctx)
			c.poll(ctx)
			c.poll(ctx)
			assert.Equal(t, want, postedReasons(post), "every event must be posted exactly once in history order")
		})
	}
}

func TestPollRetriesFirstEventAfterReset(t *testing.T) {
	c, alerts, hist, post := newTestCommand()
	ctx := context.Background()
	c.register(ctx, "g", net, &discordgo.Channel{ID: "chan"})
	hist.events = []rolloor.Event{
		{ID: 1, Action: "rollout.created", Reason: "new rollout"},
		{ID: 2, Action: "rollout.complete", Reason: "new completion"},
	}
	post.failAt = 1

	c.poll(ctx)
	saved, err := alerts.Get(ctx, net, "g")
	require.NoError(t, err)
	assert.Equal(t, new(int64(0)), saved.LastEventID)
	assert.Empty(t, post.sent)

	post.failAt = 0
	c.poll(ctx)
	c.poll(ctx)
	assert.Equal(t, []string{"new rollout", "new completion"}, postedReasons(post))
}

func TestCurrentLifecycleLabels(t *testing.T) {
	cases := []struct {
		action string
		title  string
	}{
		{"rollout.created", "🚀 Rollout created"},
		{"batch.started", "🚀 Batch started"},
		{"batch.passed", "✅ Batch passed"},
		{"rollout.soaking", "⏳ Rollout soaking"},
		{"rollout.paused", "⏸️ Rollout paused"},
		{"rollout.waitingforbudget", "⏳ Rollout waiting for disruption budget"},
		{"rollout.halted", "🛑 Rollout halted"},
		{"rollout.aborted", "⏹️ Rollout aborted"},
		{"rollout.superseded", "↪️ Rollout superseded"},
		{"rollout.complete", "✅ Rollout complete"},
	}

	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			c, _, hist, post := newTestCommand()
			ctx := context.Background()
			c.register(ctx, "g", net, &discordgo.Channel{ID: "chan"})
			hist.events = append(hist.events, rolloor.Event{ID: 6, Action: tc.action, Group: "lighthouse", Reason: "controller decision"})
			c.poll(ctx)

			require.Len(t, post.sent, 1)
			assert.Equal(t, tc.title+": lighthouse", post.sent[0].Embeds[0].Title)
			if tc.action == "rollout.halted" {
				assert.Equal(t, "<@&123>", post.sent[0].Content)
			} else {
				assert.Empty(t, post.sent[0].Content)
			}
		})
	}
}

func TestPollIgnoresObsoleteStatesAndAuditActions(t *testing.T) {
	c, alerts, hist, post := newTestCommand()
	ctx := context.Background()
	c.register(ctx, "g", net, &discordgo.Channel{ID: "chan"})
	hist.events = append(hist.events,
		rolloor.Event{ID: 6, Action: "rollout.waitingforenvironment"},
		rolloor.Event{ID: 7, Action: "rollout.waitingforsync"},
		rolloor.Event{ID: 8, Action: "refresh"},
	)

	c.poll(ctx)
	assert.Empty(t, post.sent)
	saved, err := alerts.Get(ctx, net, "g")
	require.NoError(t, err)
	assert.Equal(t, new(int64(8)), saved.LastEventID)
}
