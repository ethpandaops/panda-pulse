package mentions

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

const (
	msgEnablingMentions = "✅ Enabled mentions for **%s** on **%s**"
)

// handleEnable handles the '/mentions enable' command.
func (c *MentionsCommand) handleEnable(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
	var (
		options = data.Options
		network = options[0].StringValue()
		client  = options[1].StringValue()
		guildID = i.GuildID // Get the guild ID from the interaction
	)

	c.log.WithFields(logrus.Fields{
		"command": "/mentions enable",
		"network": network,
		"client":  client,
		"guild":   guildID,
		"user":    i.Member.User.Username,
	}).Info("Received command")

	// Get existing mentions.
	mention, err := c.bot.GetMentionsRepo().Get(context.Background(), network, client, guildID)
	if err != nil {
		return fmt.Errorf("failed to get mentions: %w", err)
	}

	// Enable mentions.
	mention.Enabled = true
	mention.UpdatedAt = time.Now()

	// Persist the updated mentions.
	if err := c.bot.GetMentionsRepo().Persist(context.Background(), mention); err != nil {
		return fmt.Errorf("failed to persist mentions: %w", err)
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(msgEnablingMentions, client, network),
		},
	})
}
