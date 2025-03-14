package checks

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

//nolint:gosec // false positive, no hardcoded credentials.
const (
	msgRunningCheck   = "🔄 Running manual check for **%s** on **%s**..."
	msgChecksPassed   = "✅ All checks passed for **%s** on **%s**"
	msgIssuesDetected = "ℹ️ Issues detected for **%s** on **%s**, see below for details"
)

// handleRun handles the '/checks run' command.
func (c *ChecksCommand) handleRun(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
	network, client := extractOptions(data)

	guildID := i.GuildID

	c.log.WithFields(logrus.Fields{
		"command": "/checks run",
		"network": network,
		"client":  client,
		"guild":   guildID,
		"user":    i.Member.User.Username,
	}).Info("Received command")

	// First respond that we're working on it.
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(msgRunningCheck, client, network),
		},
	}); err != nil {
		return fmt.Errorf("failed to send initial response: %w", err)
	}

	// Run the check using the service. We don't need to use the queue here, as
	// its just a once-off.
	alertSent, err := c.RunChecks(context.Background(), &store.MonitorAlert{
		Network:        network,
		Client:         client,
		DiscordChannel: i.ChannelID,
		DiscordGuildID: guildID,
	})
	if err != nil {
		return fmt.Errorf("failed to run checks: %w", err)
	}

	// If no alert was sent, everything is good.
	if !alertSent {
		if _, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(fmt.Sprintf(msgChecksPassed, client, network)),
		}); err != nil {
			c.log.Errorf("Failed to edit initial response: %v", err)
		}

		return nil
	}

	// Otherwise, we have issues.
	if _, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: stringPtr(fmt.Sprintf(msgIssuesDetected, client, network)),
	}); err != nil {
		c.log.Errorf("Failed to edit initial response: %v", err)
	}

	return nil
}

// extractOptions extracts command options into a structured format.
func extractOptions(data *discordgo.ApplicationCommandInteractionDataOption) (network, client string) {
	options := data.Options

	return options[0].StringValue(), options[1].StringValue()
}
