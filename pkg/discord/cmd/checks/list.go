package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/robfig/cron/v3"
)

const (
	msgNoChecksRegistered = "ℹ️ No checks are currently registered%s\n"
	msgNoChecksForNetwork = " for the network **%s**"
	msgNoChecksAnyNetwork = " for any network"
	msgNetworkClients     = "🌐 Clients registered for **%s** notifications\n"
	msgAlertsSentTo       = "Alerts are sent to "
)

// clientInfo represents registration status and channel for a client.
type clientInfo struct {
	registered bool
	channelID  string
	schedule   string
	nextRun    time.Time
}

// handleList handles the '/checks list' command.
func (c *ChecksCommand) handleList(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
	var (
		network *string
		guildID = i.GuildID
	)

	if len(data.Options) > 0 {
		n := data.Options[0].StringValue()
		network = &n
	}

	alerts, err := c.listAlerts(context.Background(), guildID, network)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	// Get all unique networks.
	networks := make(map[string]bool)

	for _, alert := range alerts {
		networks[alert.Network] = true
	}

	// If no alerts found.
	if len(networks) == 0 {
		suffix := msgNoChecksAnyNetwork

		if network != nil {
			suffix = fmt.Sprintf(msgNoChecksForNetwork, *network)
		}

		return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf(msgNoChecksRegistered, suffix),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
	}

	// First, send a deferred response to acknowledge the interaction
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send deferred response: %w", err)
	}

	// Process each network and send as follow-up messages
	var firstMessage = true

	// Then send each network's table as a separate message, we do this to get around the 2000 message limit.
	for networkName := range networks {
		if network != nil && networkName != *network {
			continue
		}

		// Create a map of registered clients for this network.
		var (
			registered = make(map[string]clientInfo)
			allClients = append(clients.CLClients, clients.ELClients...)
		)

		// Initialize all clients as unregistered.
		for _, client := range allClients {
			registered[client] = clientInfo{registered: false}
		}

		// Update with registered clients and their channels.
		for _, alert := range alerts {
			if alert.Network == networkName {
				nextRun := calculateNextRun(alert.Schedule)
				registered[alert.Client] = clientInfo{
					registered: true,
					channelID:  alert.DiscordChannel,
					schedule:   alert.Schedule,
					nextRun:    nextRun,
				}
			}
		}

		var msg strings.Builder

		msg.WriteString(fmt.Sprintf(msgNetworkClients, networkName))
		msg.WriteString(buildClientTable(allClients, registered))

		// Collect all unique channels.
		channels := make(map[string]bool)

		for _, alert := range alerts {
			if alert.Network == networkName {
				channels[alert.DiscordChannel] = true
			}
		}

		if len(channels) > 0 {
			msg.WriteString(msgAlertsSentTo)

			var first = true

			for channelID := range channels {
				if !first {
					msg.WriteString(", ")
				}

				msg.WriteString(fmt.Sprintf("<#%s>", channelID))

				first = false
			}

			msg.WriteString("\n")
		}

		// For the first network, edit the response
		if firstMessage {
			_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: pointer(msg.String()),
			})

			if err != nil {
				c.log.WithError(err).WithField("network", networkName).Error("Failed to edit response for first network")
			}

			firstMessage = false
		} else {
			// For subsequent networks, use FollowupMessageCreate
			_, err = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Content: msg.String(),
				Flags:   discordgo.MessageFlagsEphemeral,
			})

			if err != nil {
				c.log.WithError(err).WithField("network", networkName).Error("Failed to send follow-up for network")
			}
		}
	}

	return nil
}

// listAlerts lists all alerts for a given guild and optionally filtered by network.
func (c *ChecksCommand) listAlerts(ctx context.Context, guildID string, network *string) ([]*store.MonitorAlert, error) {
	alerts, err := c.bot.GetMonitorRepo().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}

	// Filter alerts for the specific guild
	guildAlerts := make([]*store.MonitorAlert, 0)

	for _, alert := range alerts {
		if alert.DiscordGuildID == guildID {
			guildAlerts = append(guildAlerts, alert)
		}
	}

	if network == nil {
		return guildAlerts, nil
	}

	// Further filter alerts for specific network.
	filtered := make([]*store.MonitorAlert, 0)

	for _, alert := range guildAlerts {
		if alert.Network == *network {
			filtered = append(filtered, alert)
		}
	}

	return filtered, nil
}

// calculateNextRun calculates the next run time based on the cron schedule.
func calculateNextRun(schedule string) time.Time {
	if schedule == "" {
		return time.Time{} // Return zero time if no schedule
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

	schedule = strings.TrimPrefix(schedule, "@every ")
	if !strings.HasPrefix(schedule, "*/") && !strings.Contains(schedule, " ") {
		// This is probably a duration like "10m" from @every, not a cron expression
		dur, err := time.ParseDuration(schedule)
		if err == nil {
			return time.Now().Add(dur)
		}
	}

	sched, err := parser.Parse(schedule)
	if err != nil {
		return time.Time{} // Return zero time if invalid schedule
	}

	return sched.Next(time.Now())
}

// buildClientTable creates an ASCII table of client statuses.
func buildClientTable(clients []string, registered map[string]clientInfo) string {
	var msg strings.Builder

	msg.WriteString("```\n")
	msg.WriteString("┌──────────────┬────────┬────────────────────┐\n")
	msg.WriteString("│ Client       │ Status │ Next Run           │\n")
	msg.WriteString("├──────────────┼────────┼────────────────────┤\n")

	for _, client := range clients {
		info := registered[client]
		status := "❌"
		nextRun := "N/A"

		if info.registered {
			status = "✅"

			if !info.nextRun.IsZero() {
				nextRun = formatNextRun(info.nextRun)
			}
		}

		msg.WriteString(fmt.Sprintf("│ %-12s │   %s   │ %-18s │\n", client, status, nextRun))
	}

	msg.WriteString("└──────────────┴────────┴────────────────────┘\n```")

	return msg.String()
}

// formatNextRun formats the next run time in a human-readable way.
func formatNextRun(t time.Time) string {
	now := time.Now()
	diff := t.Sub(now)

	if diff < 0 {
		return "Due now"
	}

	if diff < time.Minute {
		return "< 1 minute"
	}

	if diff < time.Hour {
		minutes := int(diff.Minutes())

		return fmt.Sprintf("%d min", minutes)
	}

	if diff < 24*time.Hour {
		hours := int(diff.Hours())
		minutes := int(diff.Minutes()) % 60

		return fmt.Sprintf("%dh %dm", hours, minutes)
	}

	days := int(diff.Hours() / 24)
	hours := int(diff.Hours()) % 24

	return fmt.Sprintf("%dd %dh", days, hours)
}

// pointer returns a pointer to the given string.
func pointer(s string) *string {
	return &s
}
