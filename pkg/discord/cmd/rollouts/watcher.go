package rollouts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/ethpandaops/panda-pulse/pkg/rolloor"
	"github.com/ethpandaops/panda-pulse/pkg/store"
)

// Start watches every registered network until Stop. The bot calls it.
func (c *RolloutsCommand) Start(ctx context.Context) error {
	c.stop = make(chan struct{})
	c.wg.Add(1)

	go func() {
		defer c.wg.Done()

		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			// Keep the answer to "which devnets run rolloor" warm for
			// autocomplete.
			c.runningRolloor(ctx, c.activeNetworks())
			c.poll(ctx)

			select {
			case <-ctx.Done():
				return
			case <-c.stop:
				return
			case <-ticker.C:
			}
		}
	}()

	return nil
}

// Stop ends the watcher and waits for it.
func (c *RolloutsCommand) Stop() {
	if c.stop != nil {
		close(c.stop)
		c.wg.Wait()
	}
}

// poll reads each registered network's new events, posts the ones people
// care about, and remembers how far it got. A network that stopped
// answering is skipped until it answers again.
func (c *RolloutsCommand) poll(ctx context.Context) {
	alerts, err := c.alerts.List(ctx)
	if err != nil {
		c.log.WithError(err).Warn("Failed to list rollout alerts")

		return
	}

	for _, alert := range alerts {
		c.pollOne(ctx, alert)
	}
}

func (c *RolloutsCommand) pollOne(ctx context.Context, alert *store.RolloutAlert) {
	log := c.log.WithField("network", alert.Network)

	pos, err := c.rolloor.Latest(ctx, alert.Network)
	if err != nil {
		log.WithError(err).Debug("rolloor did not answer")

		return
	}

	if alert.LastEventID == nil {
		alert.LastEventID, alert.HistoryID = new(pos.ID), pos.History
		c.save(ctx, alert)

		return
	}

	reset := replaced(alert, &pos)
	if reset {
		// A recreated history starts at one, even if it is still empty.
		alert.LastEventID = new(int64(0))
	}

	if reset || alert.HistoryID != pos.History {
		alert.HistoryID = pos.History

		if !c.save(ctx, alert) {
			return
		}
	}

	page, err := c.rolloor.History(ctx, alert.Network, *alert.LastEventID, pageSize)
	if err != nil {
		log.WithError(err).Debug("rolloor did not answer")

		return
	}

	// Replaced since Latest: the next poll starts the new history from its
	// beginning.
	if page.History != "" && page.History != alert.HistoryID {
		return
	}

	events := page.Events
	if len(events) == 0 {
		return
	}

	last := *alert.LastEventID

	for i := range events {
		if msg := c.message(ctx, alert, &events[i]); msg != nil {
			if _, err := c.post().ChannelMessageSendComplex(alert.DiscordChannel, msg, discordgo.WithContext(ctx)); err != nil {
				// Keep the failed event ahead of all later events.
				log.WithError(err).Warn("Failed to post rollout event")

				break
			}
		}

		last = events[i].ID
	}

	if last != *alert.LastEventID {
		alert.LastEventID = new(last)
		c.save(ctx, alert)
	}
}

// replaced reports whether the history alert follows has been replaced by
// another: by its identity when rolloor gives one, otherwise by the newest
// event id falling behind the cursor.
func replaced(alert *store.RolloutAlert, pos *rolloor.Position) bool {
	if alert.HistoryID != "" && pos.History != "" {
		return alert.HistoryID != pos.History
	}

	return *alert.LastEventID > pos.ID
}

func (c *RolloutsCommand) save(ctx context.Context, alert *store.RolloutAlert) bool {
	alert.UpdatedAt = time.Now()

	if err := c.alerts.Persist(ctx, alert); err != nil {
		c.log.WithError(err).WithField("network", alert.Network).Warn("Failed to save rollout cursor")

		return false
	}

	return true
}

// Running progress emits batch.started, not rollout.running. Config pauses
// do not emit rollout.paused.
var kinds = map[string]struct {
	title string
	color int
}{
	"rollout.created":          {"🚀 Rollout created", 0x4ea8ff},
	"batch.started":            {"🚀 Batch started", 0x4ea8ff},
	"batch.passed":             {"✅ Batch passed", 0x39d17c},
	"rollout.soaking":          {"⏳ Rollout soaking", 0x4ea8ff},
	"rollout.paused":           {"⏸️ Rollout paused", 0xffb547},
	"rollout.waitingforbudget": {"⏳ Rollout waiting for disruption budget", 0xffb547},
	"rollout.halted":           {"🛑 Rollout halted", 0xff4b3e},
	"rollout.complete":         {"✅ Rollout complete", 0x39d17c},
	"rollout.superseded":       {"↪️ Rollout superseded", 0x8a8f98},
	"rollout.aborted":          {"⏹️ Rollout aborted", 0x8a8f98},
}

// message builds the post for an event, or nil for events not posted.
func (c *RolloutsCommand) message(ctx context.Context, alert *store.RolloutAlert, e *rolloor.Event) *discordgo.MessageSend {
	kind, ok := kinds[e.Action]
	if !ok {
		return nil
	}

	link := c.rolloor.URL(alert.Network)
	if e.Rollout != "" {
		link += "/rollouts/" + e.Rollout
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("%s: %s", kind.title, e.Group),
		Description: e.Reason,
		URL:         link,
		Color:       kind.color,
		Timestamp:   e.At.UTC().Format(time.RFC3339),
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Network", Value: alert.Network, Inline: true},
			{Name: "Group", Value: fallback(e.Group), Inline: true},
			{Name: "By", Value: fallback(e.Actor), Inline: true},
		},
		Footer: &discordgo.MessageEmbedFooter{Text: "rolloor " + fallback(e.Rollout)},
	}

	msg := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Open in rolloor", Style: discordgo.LinkButton, URL: link},
		}}},
	}

	if e.Action == "rollout.halted" {
		if m, err := c.mentions().Get(ctx, alert.Network, e.Group, alert.DiscordGuildID); err == nil && m != nil && m.Enabled && len(m.Mentions) > 0 {
			msg.Content = strings.Join(m.Mentions, " ")
		}
	}

	return msg
}

func fallback(s string) string {
	if s == "" {
		return "-"
	}

	return s
}
