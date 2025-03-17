package checks

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/sirupsen/logrus"
)

// handleHiveRun handles the '/checks hive-run' command.
func (c *ChecksCommand) handleHiveRun(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
	network := data.Options[0].StringValue()
	guildID := i.GuildID

	c.log.WithFields(logrus.Fields{
		"command": "/checks hive-run",
		"network": network,
		"guild":   guildID,
		"user":    i.Member.User.Username,
	}).Info("Received command")

	// First respond that we're working on it.
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("🔄 Running Hive summary for **%s**...", network),
		},
	}); err != nil {
		return fmt.Errorf("failed to send initial response: %w", err)
	}

	// Create a temporary alert for this run
	alert := &hive.HiveSummaryAlert{
		Network:        network,
		DiscordChannel: i.ChannelID,
		DiscordGuildID: guildID,
		Enabled:        true,
	}

	// Run the Hive summary check
	err := c.RunHiveSummary(context.Background(), alert)
	if err != nil {
		// Edit the response to show the error
		if _, editErr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(fmt.Sprintf("❌ Failed to run Hive summary for **%s**: %v", network, err)),
		}); editErr != nil {
			c.log.Errorf("Failed to edit initial response: %v", editErr)
		}
		return fmt.Errorf("failed to run Hive summary: %w", err)
	}

	// Edit the response to show success
	if _, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: stringPtr(fmt.Sprintf("✅ Hive summary for **%s** completed successfully", network)),
	}); err != nil {
		c.log.Errorf("Failed to edit initial response: %v", err)
	}

	return nil
}
