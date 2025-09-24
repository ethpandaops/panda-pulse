package hive

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
)

// handleRun handles the run subcommand.
func (c *HiveCommand) handleRun(s *discordgo.Session, i *discordgo.InteractionCreate, cmd *discordgo.ApplicationCommandInteractionDataOption) {
	var (
		network = cmd.Options[0].StringValue()
		suite   = ""
		guildID = i.GuildID
	)

	// Extract the suite parameter if provided
	for _, opt := range cmd.Options {
		if opt.Name == optionNameSuite {
			suite = opt.StringValue()

			break
		}
	}

	// Check if Hive is available for this network.
	available, err := c.bot.GetHive().IsAvailable(context.Background(), network)
	if err != nil {
		c.respondWithError(s, i, fmt.Sprintf("Failed to check Hive availability: %v", err))

		return
	}

	if !available {
		c.respondWithError(s, i, fmt.Sprintf("🚫 Hive is not available for network **%s**", network))

		return
	}

	// Build the initial response message
	initialMsg := fmt.Sprintf("🔄 Running Hive summary for **%s**", network)
	if suite != "" {
		initialMsg = fmt.Sprintf("🔄 Running Hive summary for **%s** (suite: %s)", network, suite)
	}

	// Now, respond that we're working on it.
	if respondErr := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: initialMsg + "...",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	}); respondErr != nil {
		c.log.WithError(respondErr).Error("Failed to send initial response")

		return
	}

	// Create a temporary alert for this run
	alert := &hive.HiveSummaryAlert{
		Network:        network,
		Suite:          suite,
		DiscordChannel: i.ChannelID,
		DiscordGuildID: guildID,
		Enabled:        true,
	}

	// Run the Hive summary check.
	if runErr := c.RunHiveSummary(context.Background(), alert); runErr != nil {
		// Edit the response to show the error.
		errorMsg := fmt.Sprintf("❌ Failed to run Hive summary for **%s**", network)
		if suite != "" {
			errorMsg = fmt.Sprintf("❌ Failed to run Hive summary for **%s** (suite: %s)", network, suite)
		}

		if _, editErr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(fmt.Sprintf("%s: %v", errorMsg, runErr)),
		}); editErr != nil {
			c.log.WithError(editErr).Error("Failed to edit initial response")
		}

		c.log.WithError(runErr).Error("Failed to run Hive summary")

		return
	}

	// Edit the response to show success.
	successMsg := fmt.Sprintf("✅ Hive summary for **%s** completed successfully", network)
	if suite != "" {
		successMsg = fmt.Sprintf("✅ Hive summary for **%s** (suite: %s) completed successfully", network, suite)
	}

	if _, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: stringPtr(successMsg),
	}); err != nil {
		c.log.WithError(err).Error("Failed to edit initial response")
	}
}
