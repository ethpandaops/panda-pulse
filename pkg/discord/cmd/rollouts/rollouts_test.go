package rollouts

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/panda-pulse/pkg/rolloor"
	"github.com/ethpandaops/panda-pulse/pkg/store"
)

const net = "glamsterdam-devnet-12"

type fakeAlerts struct {
	mu     sync.Mutex
	alerts map[string]*store.RolloutAlert
	fail   error
}

func (f *fakeAlerts) key(network, guild string) string { return network + "/" + guild }

func (f *fakeAlerts) List(context.Context) ([]*store.RolloutAlert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail != nil {
		return nil, f.fail
	}

	out := make([]*store.RolloutAlert, 0, len(f.alerts))
	for _, a := range f.alerts {
		cp := *a
		out = append(out, &cp)
	}

	return out, nil
}

func (f *fakeAlerts) Get(_ context.Context, network, guild string) (*store.RolloutAlert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail != nil {
		return nil, f.fail
	}

	if a, ok := f.alerts[f.key(network, guild)]; ok {
		cp := *a

		return &cp, nil
	}

	return nil, store.ErrRolloutAlertNotFound
}

func (f *fakeAlerts) Persist(_ context.Context, a *store.RolloutAlert) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail != nil {
		return f.fail
	}

	cp := *a
	f.alerts[f.key(a.Network, a.DiscordGuildID)] = &cp

	return nil
}

func (f *fakeAlerts) Purge(_ context.Context, ids ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail != nil {
		return f.fail
	}

	delete(f.alerts, f.key(ids[0], ids[1]))

	return nil
}

type fakeHistory struct {
	mu      sync.Mutex
	running map[string]bool
	events  []rolloor.Event
	fail    error
}

func (f *fakeHistory) Available(_ context.Context, network string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.running[network]
}

func (f *fakeHistory) History(_ context.Context, _ string, after int64, _ int) ([]rolloor.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail != nil {
		return nil, f.fail
	}

	var out []rolloor.Event

	for _, e := range f.events {
		if e.ID > after {
			out = append(out, e)
		}
	}

	return out, nil
}

func (f *fakeHistory) Latest(context.Context, string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail != nil {
		return 0, f.fail
	}

	if len(f.events) == 0 {
		return 0, nil
	}

	return f.events[len(f.events)-1].ID, nil
}

func (f *fakeHistory) URL(network string) string { return "https://rolloor." + network + ".example" }

type fakePoster struct {
	mu     sync.Mutex
	sent   []*discordgo.MessageSend
	failAt int
}

func (f *fakePoster) ChannelMessageSendComplex(_ string, m *discordgo.MessageSend, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failAt > 0 && len(f.sent)+1 == f.failAt {
		return nil, errors.New("discord down")
	}

	f.sent = append(f.sent, m)

	return &discordgo.Message{}, nil
}

type fakeMentions struct{ m *store.ClientMention }

func (f fakeMentions) Get(context.Context, string, string, string) (*store.ClientMention, error) {
	return f.m, nil
}

func newTestCommand() (*RolloutsCommand, *fakeAlerts, *fakeHistory, *fakePoster) {
	alerts := &fakeAlerts{alerts: map[string]*store.RolloutAlert{}}
	hist := &fakeHistory{running: map[string]bool{net: true}, events: []rolloor.Event{{ID: 5, Action: "rollout.complete", Group: "lighthouse"}}}
	post := &fakePoster{}

	c := &RolloutsCommand{
		log: logrus.New(), alerts: alerts, rolloor: hist,
		post: func() poster { return post },
		mentions: func() mentionSource {
			return fakeMentions{m: &store.ClientMention{Enabled: true, Mentions: []string{"<@&123>"}}}
		},
		activeNetworks: func() []string { return []string{net, "glamsterdam-devnet-9", "fusaka-devnet-3"} },
		registrations:  map[string]string{},
	}

	return c, alerts, hist, post
}

func newCommandOnly() *RolloutsCommand {
	c, _, _, _ := newTestCommand() //nolint:dogsled // only the command is needed here

	return c
}

func TestRegisterOnlyWhereRolloorRuns(t *testing.T) {
	c, alerts, hist, _ := newTestCommand()
	ctx := context.Background()
	ch := &discordgo.Channel{ID: "chan"}

	assert.Contains(t, c.register(ctx, "g", "glamsterdam-devnet-9", ch), "rolloor is not running on **glamsterdam-devnet-9**")
	assert.Contains(t, c.register(ctx, "g", net, nil), "text channel")

	assert.Contains(t, c.register(ctx, "g", net, ch), "Posting **"+net+"** rollouts to <#chan>")
	saved, err := alerts.Get(ctx, net, "g")
	require.NoError(t, err)
	assert.Equal(t, int64(5), saved.LastEventID, "nothing already past is posted")

	// Registering again keeps when it was first made.
	first := saved.CreatedAt
	time.Sleep(time.Millisecond)
	c.register(ctx, "g", net, &discordgo.Channel{ID: "other"})
	saved, _ = alerts.Get(ctx, net, "g")
	assert.Equal(t, first, saved.CreatedAt)
	assert.Equal(t, "other", saved.DiscordChannel)

	assert.Contains(t, c.list(ctx, "g"), "**"+net+"** in <#other>")
	assert.Contains(t, c.list(ctx, "elsewhere"), "No devnet")

	assert.Contains(t, c.deregister(ctx, "g", net), "Stopped posting")
	assert.Contains(t, c.deregister(ctx, "g", net), "not posted here")

	hist.fail = errors.New("boom")
	assert.Contains(t, c.register(ctx, "g", net, ch), "Could not read rolloor")

	hist.fail = nil
	alerts.fail = errors.New("s3 down")
	assert.Contains(t, c.register(ctx, "g", net, ch), "Could not save")
	assert.Contains(t, c.deregister(ctx, "g", net), "Could not read")
	assert.Contains(t, c.list(ctx, "g"), "Could not read")
}

func TestNetworkChoicesAreTheDevnetsRunningRolloor(t *testing.T) {
	c := newCommandOnly()

	choices := c.networkChoices(context.Background(), "")
	require.Len(t, choices, 1)
	assert.Equal(t, net, choices[0].Value)

	assert.Empty(t, c.networkChoices(context.Background(), "fusaka"))
}

func TestPollPostsWhatPeopleCareAbout(t *testing.T) {
	c, alerts, hist, post := newTestCommand()
	ctx := context.Background()

	require.NoError(t, alerts.Persist(ctx, &store.RolloutAlert{Network: net, DiscordGuildID: "g", DiscordChannel: "chan", LastEventID: 5}))

	hist.events = append(hist.events,
		rolloor.Event{ID: 6, Action: "rollout.created", Group: "lighthouse", Rollout: "abc", Reason: "8 targets to 88f9568", At: time.Now()},
		rolloor.Event{ID: 7, Action: "batch.started", Group: "lighthouse", Rollout: "abc"},
		rolloor.Event{ID: 8, Action: "rollout.halted", Group: "lighthouse", Rollout: "abc", Reason: "node-1/beacon not ready"},
	)

	c.poll(ctx)

	require.Len(t, post.sent, 2, "batch events are not posted")
	assert.Contains(t, post.sent[0].Embeds[0].Title, "Rollout started: lighthouse")
	assert.Equal(t, "https://rolloor."+net+".example/rollouts/abc", post.sent[0].Embeds[0].URL)
	assert.Empty(t, post.sent[0].Content)
	assert.Contains(t, post.sent[1].Embeds[0].Title, "Rollout halted")
	assert.Equal(t, "<@&123>", post.sent[1].Content, "a halt pings the client team")

	saved, _ := alerts.Get(ctx, net, "g")
	assert.Equal(t, int64(8), saved.LastEventID)

	// Nothing new: nothing posted.
	c.poll(ctx)
	assert.Len(t, post.sent, 2)
}

func TestPollKeepsItsPlaceWhenDiscordFails(t *testing.T) {
	c, alerts, hist, post := newTestCommand()
	ctx := context.Background()

	require.NoError(t, alerts.Persist(ctx, &store.RolloutAlert{Network: net, DiscordGuildID: "g", DiscordChannel: "chan", LastEventID: 5}))

	hist.events = append(hist.events,
		rolloor.Event{ID: 6, Action: "rollout.created", Group: "lighthouse"},
		rolloor.Event{ID: 7, Action: "rollout.complete", Group: "lighthouse"},
	)
	post.failAt = 2

	c.poll(ctx)

	saved, _ := alerts.Get(ctx, net, "g")
	assert.Equal(t, int64(6), saved.LastEventID, "the failed post is tried again")

	post.failAt = 0
	c.poll(ctx)
	require.Len(t, post.sent, 2)
	assert.Contains(t, post.sent[1].Embeds[0].Title, "complete")
}

func TestPollStartsFromTheNewestEventAndSurvivesOutages(t *testing.T) {
	c, alerts, hist, post := newTestCommand()
	ctx := context.Background()

	// An alert saved without a cursor starts from now.
	require.NoError(t, alerts.Persist(ctx, &store.RolloutAlert{Network: net, DiscordGuildID: "g", DiscordChannel: "chan"}))
	c.poll(ctx)
	assert.Empty(t, post.sent)

	saved, _ := alerts.Get(ctx, net, "g")
	assert.Equal(t, int64(5), saved.LastEventID)

	// rolloor unreachable: nothing happens and nothing is lost.
	hist.fail = errors.New("unreachable")
	c.poll(ctx)
	saved, _ = alerts.Get(ctx, net, "g")
	assert.Equal(t, int64(5), saved.LastEventID)

	alerts.fail = errors.New("s3 down")
	c.poll(ctx)
}

func TestStartAndStop(t *testing.T) {
	c := newCommandOnly()
	ctx, cancel := context.WithCancel(context.Background())

	require.NoError(t, c.Start(ctx))
	c.Stop()

	require.NoError(t, c.Start(ctx))
	cancel()
	c.wg.Wait()

	(&RolloutsCommand{}).Stop()
	assert.Equal(t, "rollouts", c.Name())
	assert.Len(t, c.definition().Options, 3)
}
