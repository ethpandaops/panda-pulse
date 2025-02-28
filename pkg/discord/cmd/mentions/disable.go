package mentions

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

const (
	msgDisablingMentions = "✅ Disabled mentions for **%s** on **%s**"
)

// handleDisable handles the '/mentions disable' command.
func (c *MentionsCommand) handleDisable(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
	var (
		options = data.Options
		network = options[0].StringValue()
		client  = options[1].StringValue()
	)

	c.log.WithFields(logrus.Fields{
		"command": "/mentions disable",
		"network": network,
		"client":  client,
		"user":    i.Member.User.Username,
	}).Info("Received command")

	// Get existing mentions or create new.
	mention, err := c.bot.GetMentionsRepo().Get(context.Background(), network, client)
	if err != nil {
		// If not found, create new.
		mention = &store.ClientMention{
			Network:   network,
			Client:    client,
			Mentions:  []string{},
			Enabled:   false,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	} else {
		mention.Enabled = false
		mention.UpdatedAt = time.Now()
	}

	// Persist the updated mentions.
	if err := c.bot.GetMentionsRepo().Persist(context.Background(), mention); err != nil {
		return fmt.Errorf("failed to persist mentions: %w", err)
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(msgDisablingMentions, client, network),
		},
	})
}
