package hive

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/robfig/cron/v3"
)

const (
	msgNoHiveSummariesRegistered = "ℹ️ No Hive summaries are currently registered%s\n"
	msgNoHiveSummariesForNetwork = " for the network **%s**"
	msgNoHiveSummariesAnyNetwork = " for any network"
	msgNetworkHiveSummary        = "🌐 Hive summary registered for **%s**\n"
	msgAlertsSentTo              = "Alerts are sent to "
)

// handleList handles the '/hive list' command.
func (c *HiveCommand) handleList(
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
		suffix := msgNoHiveSummariesAnyNetwork

		if network != nil {
			suffix = fmt.Sprintf(msgNoHiveSummariesForNetwork, *network)
		}

		return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf(msgNoHiveSummariesRegistered, suffix),
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

	for networkName := range networks {
		if network != nil && networkName != *network {
			continue
		}

		var msg strings.Builder

		msg.WriteString(fmt.Sprintf(msgNetworkHiveSummary, networkName))
		msg.WriteString(buildSummaryTable(alerts, networkName))

		// Find the channel for this network
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

// listAlerts lists all Hive summary alerts for a given guild and optionally filtered by network.
func (c *HiveCommand) listAlerts(ctx context.Context, guildID string, network *string) ([]*hive.HiveSummaryAlert, error) {
	alerts, err := c.bot.GetHiveSummaryRepo().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}

	// Filter alerts for the specific guild
	guildAlerts := make([]*hive.HiveSummaryAlert, 0)

	for _, alert := range alerts {
		if alert.DiscordGuildID == guildID {
			guildAlerts = append(guildAlerts, alert)
		}
	}

	if network == nil {
		return guildAlerts, nil
	}

	// Further filter alerts for specific network.
	filtered := make([]*hive.HiveSummaryAlert, 0)

	for _, alert := range guildAlerts {
		if alert.Network == *network {
			filtered = append(filtered, alert)
		}
	}

	return filtered, nil
}

// buildSummaryTable creates an ASCII table of Hive summary status.
func buildSummaryTable(alerts []*hive.HiveSummaryAlert, networkName string) string {
	var msg strings.Builder

	msg.WriteString("```\n")
	msg.WriteString("┌──────────────────┬────────┬────────────────────┐\n")
	msg.WriteString("│ Network          │ Status │ Next Run           │\n")
	msg.WriteString("├──────────────────┼────────┼────────────────────┤\n")

	for _, alert := range alerts {
		if alert.Network != networkName {
			continue
		}

		status := "❌"
		nextRun := "N/A"

		if alert.Enabled {
			status = "✅"

			nextRunTime := calculateNextRun(alert.Schedule)
			if !nextRunTime.IsZero() {
				nextRun = formatNextRun(nextRunTime)
			}
		}

		msg.WriteString(fmt.Sprintf("│ %-16s │   %s   │ %-18s │\n", alert.Network, status, nextRun))
	}

	msg.WriteString("└──────────────────┴────────┴────────────────────┘\n```")

	return msg.String()
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
