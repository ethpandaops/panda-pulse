package mentions

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

const (
	msgRemovingMentions = "✅ Removing mentions for **%s** on **%s**: %s"
	msgNoMentionsFound  = "ℹ️ No mentions found for **%s** on **%s**"
)

// handleRemove handles the '/mentions remove' command.
func (c *MentionsCommand) handleRemove(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
	var (
		options  = data.Options
		network  = options[0].StringValue()
		client   = options[1].StringValue()
		mentions = strings.Fields(options[2].StringValue()) // Split on whitespace
	)

	c.log.WithFields(logrus.Fields{
		"command":  "/mentions remove",
		"network":  network,
		"client":   client,
		"mentions": mentions,
		"user":     i.Member.User.Username,
	}).Info("Received command")

	// Get existing mentions.
	mention, err := c.bot.GetMentionsRepo().Get(context.Background(), network, client)
	if err != nil {
		return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf(msgNoMentionsFound, client, network),
			},
		})
	}

	// Remove mentions.
	for _, m := range mentions {
		mention.Mentions = removeFromSlice(mention.Mentions, m)
	}

	mention.UpdatedAt = time.Now()

	// Persist the updated mentions.
	if err := c.bot.GetMentionsRepo().Persist(context.Background(), mention); err != nil {
		return fmt.Errorf("failed to persist mentions: %w", err)
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(msgRemovingMentions, client, network, strings.Join(mentions, " ")),
		},
	})
}

// removeFromSlice removes a string from a slice.
func removeFromSlice(slice []string, str string) []string {
	var result []string

	for _, s := range slice {
		if s != str {
			result = append(result, s)
		}
	}

	return result
}
