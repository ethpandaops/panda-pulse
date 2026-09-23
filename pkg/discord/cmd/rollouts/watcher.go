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

	if alert.LastEventID == 0 {
		latest, err := c.rolloor.Latest(ctx, alert.Network)
		if err != nil || latest == 0 {
			return
		}

		alert.LastEventID = latest
		c.save(ctx, alert)

		return
	}

	events, err := c.rolloor.History(ctx, alert.Network, alert.LastEventID, pageSize)
	if err != nil {
		log.WithError(err).Debug("rolloor did not answer")

		return
	}

	if len(events) == 0 {
		return
	}

	for i := range events {
		msg := c.message(ctx, alert, &events[i])
		if msg == nil {
			continue
		}

		if _, err := c.post().ChannelMessageSendComplex(alert.DiscordChannel, msg); err != nil {
			// Try this event again next time rather than skip it.
			log.WithError(err).Warn("Failed to post rollout event")

			if i > 0 {
				alert.LastEventID = events[i-1].ID
				c.save(ctx, alert)
			}

			return
		}
	}

	alert.LastEventID = events[len(events)-1].ID
	c.save(ctx, alert)
}

func (c *RolloutsCommand) save(ctx context.Context, alert *store.RolloutAlert) {
	alert.UpdatedAt = time.Now()

	if err := c.alerts.Persist(ctx, alert); err != nil {
		c.log.WithError(err).WithField("network", alert.Network).Warn("Failed to save rollout cursor")
	}
}

// kinds are the events posted, with their title and colour.
var kinds = map[string]struct {
	title string
	color int
}{
	"rollout.created":    {"🚀 Rollout started", 0x4ea8ff},
	"rollout.paused":     {"⏸️ Rollout paused", 0xffb547},
	"rollout.halted":     {"🛑 Rollout halted", 0xff4b3e},
	"rollout.complete":   {"✅ Rollout complete", 0x39d17c},
	"rollout.superseded": {"↪️ Rollout superseded by a newer build", 0x8a8f98},
	"rollout.aborted":    {"⏹️ Rollout aborted", 0x8a8f98},
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
