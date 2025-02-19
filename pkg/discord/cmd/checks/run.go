package checks

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/store"
)

// handleRun handles the '/checks run' command.
func (c *ChecksCommand) handleRun(s *discordgo.Session, i *discordgo.InteractionCreate, data *discordgo.ApplicationCommandInteractionDataOption) error {
	options := data.Options
	network := options[0].StringValue()
	client := options[1].StringValue()

	c.log.Infof("Received checks run command: network=%s client=%s from user=%s",
		network, client, i.Member.User.Username)

	// First respond that we're working on it.
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("🔄 Running manual check for **%s** on **%s**...", client, network),
		},
	}); err != nil {
		return fmt.Errorf("failed to send initial response: %w", err)
	}

	// Create a temporary alert.
	tempAlert := &store.MonitorAlert{
		Network:        network,
		Client:         client,
		DiscordChannel: i.ChannelID,
	}

	// Run the check using the service.
	alertSent, err := c.RunChecks(context.Background(), tempAlert)
	if err != nil {
		return fmt.Errorf("failed to run checks: %w", err)
	}

	if !alertSent {
		// If no alert was sent, everything is good..
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(fmt.Sprintf("✅ All checks passed for **%s** on **%s**", client, network)),
		})
		if err != nil {
			c.log.Printf("Failed to edit initial response: %v", err)
		}
	} else {
		// Otherwise, we have issues.
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(fmt.Sprintf("🚫 Issues detected for **%s** on **%s**, see below for details", client, network)),
		})
		if err != nil {
			c.log.Errorf("Failed to edit initial response: %v", err)
		}
	}

	return nil
}
