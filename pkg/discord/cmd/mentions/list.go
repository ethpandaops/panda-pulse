package mentions

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

const (
	msgNoMentionsRegistered = "ℹ️ No mentions are currently registered%s\n"
	msgNoMentionsForNetwork = " for the network **%s**"
	msgNoMentionsAnyNetwork = " for any network"
	msgNetworkMentions      = "🌐 Mentions registered for **%s**\n"
)

// handleList handles the '/mentions list' command.
func (c *MentionsCommand) handleList(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
	var network *string

	if len(data.Options) > 0 {
		n := data.Options[0].StringValue()
		network = &n
	}

	c.log.WithFields(logrus.Fields{
		"command": "/mentions list",
		"user":    i.Member.User.Username,
	}).Info("Received command")

	mentions, err := c.listMentions(context.Background(), network)
	if err != nil {
		return fmt.Errorf("failed to list mentions: %w", err)
	}

	// Get all unique networks.
	networks := make(map[string]bool)
	for _, mention := range mentions {
		networks[mention.Network] = true
	}

	// If no mentions found.
	if len(networks) == 0 {
		suffix := msgNoMentionsAnyNetwork

		if network != nil {
			suffix = fmt.Sprintf(msgNoMentionsForNetwork, *network)
		}

		return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf(msgNoMentionsRegistered, suffix),
			},
		})
	}

	// First, respond to the interaction to acknowledge it.
	if err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Listing mentions...",
		},
	}); err != nil {
		return fmt.Errorf("failed to respond to interaction: %w", err)
	}

	// Then send each network's table as a separate message, we do this to get around the 2000 message limit.
	for networkName := range networks {
		if network != nil && networkName != *network {
			continue
		}

		// Group mentions by client.
		clientMentions := make(map[string]*store.ClientMention)

		for _, mention := range mentions {
			if mention.Network == networkName {
				// Resolve mention IDs to names.
				mentionCopy := *mention
				mentionCopy.Mentions = c.resolveMentions(s, i.GuildID, mention.Mentions)
				clientMentions[mention.Client] = &mentionCopy
			}
		}

		msg := fmt.Sprintf(msgNetworkMentions, networkName) + buildMentionsTable(clientMentions)

		if _, err := s.ChannelMessageSend(i.ChannelID, msg); err != nil {
			c.log.WithError(err).WithField("network", networkName).Error("Failed to send network mentions table")
		}
	}

	return nil
}

// listMentions lists all mentions for a given network.
func (c *MentionsCommand) listMentions(ctx context.Context, network *string) ([]*store.ClientMention, error) {
	mentions, err := c.bot.GetMentionsRepo().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list mentions: %w", err)
	}

	if network == nil {
		return mentions, nil
	}

	// Filter mentions for specific network.
	filtered := make([]*store.ClientMention, 0)

	for _, mention := range mentions {
		if mention.Network == *network {
			filtered = append(filtered, mention)
		}
	}

	return filtered, nil
}

// buildMentionsTable creates an ASCII table of client mentions.
func buildMentionsTable(mentions map[string]*store.ClientMention) string {
	var msg strings.Builder

	// Get all available clients.
	allClients := append(clients.CLClients, clients.ELClients...)
	sort.Strings(allClients)

	msg.WriteString("```\n")
	msg.WriteString("┌──────────────┬───────────────────────────┬─────────┐\n")
	msg.WriteString("│ Client       │ Mentions                  │ Enabled │\n")
	msg.WriteString("├──────────────┼───────────────────────────┼─────────┤\n")

	for _, client := range allClients {
		var (
			mention, exists = mentions[client]
			mentionsStr     = ""
			status          = "❌"
		)

		if exists {
			mentionsStr = strings.Join(mention.Mentions, " ")
			if len(mentionsStr) > 25 {
				mentionsStr = mentionsStr[:22] + "..."
			}

			if mention.Enabled {
				status = "✅"
			}
		}

		msg.WriteString(fmt.Sprintf("│ %-12s │ %-25s │   %s   │\n", client, mentionsStr, status))
	}

	msg.WriteString("└──────────────┴───────────────────────────┴─────────┘\n```")

	return msg.String()
}

// resolveMentions converts mention IDs to readable names - discord does not render them within codeblocks nicely, so
// we need to resolve them to their actual names.
func (c *MentionsCommand) resolveMentions(s *discordgo.Session, guildID string, mentions []string) []string {
	resolved := make([]string, 0)

	for _, mention := range mentions {
		// Strip < > and @ from the mention ID.
		id := strings.TrimPrefix(strings.TrimSuffix(mention, ">"), "<@")
		id = strings.TrimPrefix(id, "&") // This is required for role mentions.

		// Try to resolve as role first.
		if role, err := s.State.Role(guildID, id); err == nil {
			resolved = append(resolved, "@"+role.Name)

			continue
		}

		// Then try as user.
		if user, err := s.User(id); err == nil {
			resolved = append(resolved, "@"+user.Username)

			continue
		}

		// If we can't resolve it, use the original mention.
		resolved = append(resolved, mention)
	}

	return resolved
}
