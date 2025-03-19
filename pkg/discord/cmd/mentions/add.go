package mentions

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

const (
	msgAddingMentions = "✅ Adding mentions for **%s** on **%s**: %s"
)

// handleAdd handles the '/mentions add' command.
func (c *MentionsCommand) handleAdd(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
	var (
		options  = data.Options
		network  = options[0].StringValue()
		client   = options[1].StringValue()
		mentions = strings.Fields(options[2].StringValue()) // Split on whitespace
		guildID  = i.GuildID                                // Get the guild ID from the interaction
	)

	c.log.WithFields(logrus.Fields{
		"command":  "/mentions add",
		"network":  network,
		"client":   client,
		"guild":    guildID,
		"mentions": mentions,
		"user":     i.Member.User.Username,
	}).Info("Received command")

	// Get existing mentions or create new.
	mention, err := c.bot.GetMentionsRepo().Get(context.Background(), network, client, guildID)
	if err != nil {
		// If not found, create new.
		mention = &store.ClientMention{
			Network:        network,
			Client:         client,
			DiscordGuildID: guildID,
			Mentions:       []string{},
			Enabled:        true,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
	}

	// Add new mentions, avoiding duplicates.
	for _, m := range mentions {
		if !contains(mention.Mentions, m) {
			mention.Mentions = append(mention.Mentions, m)
		}
	}

	mention.UpdatedAt = time.Now()

	// Persist the updated mentions.
	if err := c.bot.GetMentionsRepo().Persist(context.Background(), mention); err != nil {
		return fmt.Errorf("failed to persist mentions: %w", err)
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(msgAddingMentions, client, network, strings.Join(mentions, " ")),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// contains checks if a string slice contains a string.
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}

	return false
}
